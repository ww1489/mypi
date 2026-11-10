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

// version 是 --version 打印的版本号。打印它不需要读任何配置。
const version = "0.0.1"

// defaultModel 是默认模型名。faux provider 会把它写进录制文件，
// 用来确认参数确实传到了 provider。
const defaultModel = "faux-1"

// 退出码。测试会直接断言这几个值。
const (
	exitOK    = 0
	exitError = 1
	exitUsage = 2
)

const usageText = `mypi — a minimal coding agent (teaching project)

Usage:
  mypi [flags] [prompt]

Flags:
  -h, --help        Show this help and exit
      --version     Print the version and exit
      --model NAME  Model name (default: ` + defaultModel + `)
      --provider P  Provider backend: faux (default: faux)

Environment:
  MYPY_FAUX_SCRIPT  Path to the scripted response file (built-in demo when unset)
  MYPY_FAUX_RECORD  Path appended with one JSON line per provider request
`

type options struct {
	prompt      string
	showHelp    bool
	showVersion bool
	model       string
	provider    string
}

// parseArgs 自己挨个解析参数。参数就这几个，用 flag 库不划算，
// 报错信息也能自己写清楚。
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

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
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

	// 单次提问和 REPL 都从这里走，输出和换行也只在这一处处理。
	ask := func(history []ai.Message) ([]ai.Message, error) {
		wrote = false
		next, runErr := ag.Run(ctx, history)
		if wrote {
			out.WriteString("\n")
		}
		flushErr := out.Flush()
		if runErr != nil {
			return next, runErr
		}
		return next, flushErr
	}

	if strings.TrimSpace(opts.prompt) != "" {
		_, err := ask([]ai.Message{&ai.UserMessage{Content: ai.Text(opts.prompt)}})
		if err != nil {
			fmt.Fprintf(stderr, "mypi: %v\n", err)
			return exitError
		}
		return exitOK
	}

	// 只有键盘输入才显示提示符，管道输入要保持输出干净。
	interactive := false
	if file, ok := stdin.(*os.File); ok {
		if info, statErr := file.Stat(); statErr == nil {
			interactive = info.Mode()&os.ModeCharDevice != 0
		}
	}

	scanner := bufio.NewScanner(stdin)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	var history []ai.Message
	for {
		if interactive {
			fmt.Fprint(out, "> ")
			if err := out.Flush(); err != nil {
				fmt.Fprintf(stderr, "mypi: %v\n", err)
				return exitError
			}
		}
		if !scanner.Scan() {
			break
		}
		prompt := strings.TrimSpace(scanner.Text())
		if prompt == "" {
			continue
		}

		turn := make([]ai.Message, 0, len(history)+1)
		turn = append(turn, history...)
		turn = append(turn, &ai.UserMessage{Content: ai.Text(prompt)})
		next, err := ask(turn)
		history = next
		if err != nil {
			fmt.Fprintf(stderr, "mypi: %v\n", err)
		}
	}
	if err := scanner.Err(); err != nil {
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
