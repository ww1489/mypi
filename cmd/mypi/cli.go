package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"mypi/agent"
	"mypi/ai"
)

// version 是 --version 报告的内容，它绝不读取用户配置。
const version = "0.0.1"

// defaultModel 是交给 provider 的模型名。faux provider 会原样回显，
// 足以证明整条链路已经接通。
const defaultModel = "faux-1"

// 退出码。它们是 CLI 契约的一部分，测试会据此断言。
const (
	exitOK    = 0
	exitError = 1
	exitUsage = 2
)

const usageText = `mypi — a minimal coding agent (teaching project)

Usage:
  mypi [flags] <prompt>

Flags:
  -h, --help        Show this help and exit
      --version     Print the version and exit
      --model NAME  Model name (default: ` + defaultModel + `)
      --provider P  Provider backend: faux (default: faux)

Environment:
  MYPY_FAUX_SCRIPT  Path to the scripted response file (faux provider)
  MYPY_FAUX_RECORD  Path appended with one JSON line per provider request
`

type options struct {
	prompt      string
	showHelp    bool
	showVersion bool
	model       string
	provider    string
}

// parseArgs 手写遍历参数列表。PiG 也是这么做的：对于文法如此之小的 CLI，
// flag 库带来的收益很小，而手写解析能让错误信息保持是我们自己的措辞。
func parseArgs(args []string) (options, error) {
	opts := options{model: defaultModel, provider: "faux"}
	var positional []string

	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "-h" || arg == "--help":
			opts.showHelp = true
		case arg == "--version":
			opts.showVersion = true
		case arg == "--model":
			i++
			if i >= len(args) {
				return opts, errors.New("--model needs a value")
			}
			opts.model = args[i]
		case strings.HasPrefix(arg, "--model="):
			opts.model = strings.TrimPrefix(arg, "--model=")
		case arg == "--provider":
			i++
			if i >= len(args) {
				return opts, errors.New("--provider needs a value")
			}
			opts.provider = args[i]
		case strings.HasPrefix(arg, "--provider="):
			opts.provider = strings.TrimPrefix(arg, "--provider=")
		case strings.HasPrefix(arg, "--"):
			return opts, fmt.Errorf("unknown flag %q", arg)
		default:
			positional = append(positional, arg)
		}
	}

	opts.prompt = strings.Join(positional, " ")
	return opts, nil
}

func run(args []string, stdout, stderr io.Writer) int {
	opts, err := parseArgs(args)
	if err != nil {
		fmt.Fprintf(stderr, "mypi: %v\n\n", err)
		fmt.Fprint(stderr, usageText)
		return exitUsage
	}

	if opts.showHelp {
		fmt.Fprint(stdout, usageText)
		return exitOK
	}
	if opts.showVersion {
		fmt.Fprintf(stdout, "mypi %s\n", version)
		return exitOK
	}
	if strings.TrimSpace(opts.prompt) == "" {
		fmt.Fprint(stderr, "mypi: missing prompt\n\n")
		fmt.Fprint(stderr, usageText)
		return exitUsage
	}

	provider, err := newProvider(opts.provider)
	if err != nil {
		fmt.Fprintf(stderr, "mypi: %v\n", err)
		return exitUsage
	}
	if closer, ok := provider.(io.Closer); ok {
		defer closer.Close()
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	out := bufio.NewWriter(stdout)
	wrote := false
	ag := &agent.Agent{
		Provider: provider,
		Model:    opts.model,
		OnEvent: func(ev ai.Event) {
			if delta, ok := ev.(ai.TextDeltaEvent); ok && delta.Text != "" {
				out.WriteString(delta.Text)
				wrote = true
			}
		},
	}

	if _, err := ag.Run(ctx, []ai.Message{&ai.UserMessage{Content: ai.Text(opts.prompt)}}); err != nil {
		out.Flush()
		fmt.Fprintf(stderr, "mypi: %v\n", err)
		return exitError
	}
	if wrote {
		out.WriteString("\n")
	}
	if err := out.Flush(); err != nil {
		fmt.Fprintf(stderr, "mypi: %v\n", err)
		return exitError
	}
	return exitOK
}

func newProvider(name string) (ai.Provider, error) {
	switch name {
	case "faux":
		return ai.NewFaux(), nil
	default:
		return nil, fmt.Errorf("unknown provider %q (supported: faux)", name)
	}
}
