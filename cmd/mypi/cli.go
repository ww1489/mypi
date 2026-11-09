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
  mypi [flags] <prompt>   Ask once, print the answer, exit
  mypi [flags]            Talk in a loop: one prompt per line, Ctrl-D to exit

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
	ag := &agent.Agent{
		Provider: provider,
		Model:    opts.model,
		OnEvent: func(ev ai.Event) {
			delta, ok := ev.(ai.TextDeltaEvent)
			if !ok || delta.Text == "" {
				return
			}
			out.WriteString(delta.Text)
			// 每来一段就刷一次，用户才能看到字一个个出来。
			out.Flush()
		},
	}

	// 带了问题就只问一次，没带就进交互模式。两条路走的是同一个 Agent。
	if strings.TrimSpace(opts.prompt) != "" {
		return askOnce(ctx, ag, out, stderr, opts.prompt)
	}
	return askRepeatedly(ctx, ag, out, stderr, stdin)
}

// askOnce 问一次就退出。出错返回 1，成功返回 0。
func askOnce(ctx context.Context, ag *agent.Agent, out *bufio.Writer, stderr io.Writer, prompt string) int {
	history := []ai.Message{&ai.UserMessage{Content: ai.Text(prompt)}}
	if _, err := ag.Run(ctx, history); err != nil {
		out.Flush()
		fmt.Fprintf(stderr, "mypi: %v\n", err)
		return exitError
	}
	fmt.Fprintln(out)
	if err := out.Flush(); err != nil {
		fmt.Fprintf(stderr, "mypi: %v\n", err)
		return exitError
	}
	return exitOK
}

// askRepeatedly 从 stdin 一行一行读问题，读到结尾（Ctrl-D 或管道结束）就退出。
//
// 每一轮都带上之前所有消息，模型才能"记得"前面说过什么——它自己是不记事的。
func askRepeatedly(ctx context.Context, ag *agent.Agent, out *bufio.Writer, stderr io.Writer, stdin io.Reader) int {
	var history []ai.Message
	interactive := isTerminal(stdin)

	scanner := bufio.NewScanner(stdin)
	// 默认 64KB 一行的上限太紧，粘一段长文本就报错了。
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	for {
		if interactive {
			fmt.Fprint(out, "> ")
			out.Flush()
		}
		if !scanner.Scan() {
			break
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		// 先拼出「旧历史 + 这一句」。跑失败时 Agent 也会返回带错误消息的历史，
		// 照样收下——这样上下文里留下「问过、答错了」的痕迹，而不是一片空白。
		next, err := ag.Run(ctx, append(cloneMessages(history), &ai.UserMessage{Content: ai.Text(line)}))
		history = next
		if err != nil {
			out.Flush()
			fmt.Fprintf(stderr, "mypi: %v\n", err)
			continue
		}
		fmt.Fprintln(out)
	}

	if interactive {
		// 用户按 Ctrl-D 时命令行停在同一行，补个换行才好看。
		fmt.Fprintln(out)
	}
	if err := out.Flush(); err != nil {
		fmt.Fprintf(stderr, "mypi: %v\n", err)
		return exitError
	}
	return exitOK
}

// cloneMessages 复制一份切片，避免 append 改到调用方那份。
func cloneMessages(msgs []ai.Message) []ai.Message {
	return append(make([]ai.Message, 0, len(msgs)+1), msgs...)
}

// isTerminal 判断输入是不是终端。是终端才打印 "> " 提示符，
// 从管道读的时候不该往输出里掺提示符。
func isTerminal(r io.Reader) bool {
	file, ok := r.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

func newProvider(name string) (ai.Provider, error) {
	switch name {
	case "faux":
		return ai.NewFaux(), nil
	default:
		return nil, fmt.Errorf("unknown provider %q (supported: faux)", name)
	}
}
