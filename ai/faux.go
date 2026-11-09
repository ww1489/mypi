package ai

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
)

// defaultScript 是编进二进制里的示例脚本。没有外部脚本也能直接跑起来。
//
//go:embed default_script.json
var defaultScript []byte

// faux provider 用到的环境变量。
const (
	// FauxScriptEnv 是假回复脚本的路径。
	FauxScriptEnv = "MYPY_FAUX_SCRIPT"
	// FauxRecordEnv 是录制文件的路径，每发一次请求就往里加一行 JSON。
	// 测试就是靠它看 agent 到底发了什么。
	FauxRecordEnv = "MYPY_FAUX_RECORD"
)

// FauxScript 是磁盘上的脚本文件。
//
//	{"responses":[{"chunks":["hello"," world"],"stopReason":"stop"}]}
//
// 每来一个请求就按顺序取一条。哪条写了 "error"，那个请求就直接失败。
type FauxScript struct {
	Responses []FauxResponse `json:"responses"`
}

// FauxResponse 是一条假回复。
type FauxResponse struct {
	Chunks     []string `json:"chunks"`
	StopReason string   `json:"stopReason,omitempty"`
	Error      string   `json:"error,omitempty"`
}

// Faux 是个假 provider：不连模型，直接把脚本里的回复吐出来。每次请求都记到
// 录制文件里，这样测试能检查 agent 发了什么，而不只是看它打印了什么。
//
// 创建时什么都不读，所以 --help、--version 在没有配置的机器上也能跑。
type Faux struct {
	scriptPath string
	recordPath string

	mu       sync.Mutex
	script   *FauxScript
	cursor   int
	recorder *os.File
}

// NewFaux 从环境变量读出脚本路径和录制路径。文件要等第一次请求时才读。
func NewFaux() *Faux {
	return &Faux{
		scriptPath: os.Getenv(FauxScriptEnv),
		recordPath: os.Getenv(FauxRecordEnv),
	}
}

// ID 返回后端名字。
func (f *Faux) ID() string {
	return "faux"
}

// Close 关掉录制文件。
func (f *Faux) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.recorder == nil {
		return nil
	}
	err := f.recorder.Close()
	f.recorder = nil
	return err
}

// Stream 从脚本里取一条回复，当作流式结果吐出去。
func (f *Faux) Stream(ctx context.Context, req Request) (<-chan Event, error) {
	if err := f.loadScript(); err != nil {
		return nil, err
	}

	f.mu.Lock()
	if err := f.openRecorderLocked(); err != nil {
		f.mu.Unlock()
		return nil, err
	}
	if err := f.recordLocked(req); err != nil {
		f.mu.Unlock()
		return nil, err
	}
	index := f.cursor
	f.cursor++
	var resp FauxResponse
	found := index < len(f.script.Responses)
	if found {
		resp = f.script.Responses[index]
	}
	f.mu.Unlock()

	if !found {
		return nil, fmt.Errorf("faux: script exhausted: no response for request %d (script has %d)", index+1, len(f.script.Responses))
	}

	ch := make(chan Event)
	go func() {
		defer close(ch)
		if resp.Error != "" {
			send(ctx, ch, ErrorEvent{Err: errors.New(resp.Error)})
			return
		}
		var text strings.Builder
		for _, chunk := range resp.Chunks {
			text.WriteString(chunk)
			if !send(ctx, ch, TextDeltaEvent{Text: chunk}) {
				return
			}
		}
		stop := resp.StopReason
		if stop == "" {
			stop = StopReasonStop
		}
		send(ctx, ch, MessageDoneEvent{Message: &AssistantMessage{
			Content:    Text(text.String()),
			Provider:   f.ID(),
			Model:      req.Model,
			StopReason: stop,
		}})
	}()
	return ch, nil
}

func send(ctx context.Context, ch chan<- Event, ev Event) bool {
	select {
	case <-ctx.Done():
		return false
	case ch <- ev:
		return true
	}
}

func (f *Faux) loadScript() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.script != nil {
		return nil
	}

	// 没指定脚本文件就用内置的那份，这样刚 clone 下来就能直接跑。
	source := "内置示例脚本"
	raw := defaultScript
	if f.scriptPath != "" {
		source = f.scriptPath
		read, err := os.ReadFile(f.scriptPath)
		if err != nil {
			return fmt.Errorf("faux: read script: %w", err)
		}
		raw = read
	}

	var script FauxScript
	if err := json.Unmarshal(raw, &script); err != nil {
		return fmt.Errorf("faux: parse script %s: %w", source, err)
	}
	f.script = &script
	return nil
}

// fauxRecord 是每次请求写进录制文件的那一行。
type fauxRecord struct {
	Model    string        `json:"model"`
	Messages []WireMessage `json:"messages"`
	Stream   bool          `json:"stream"`
}

func (f *Faux) openRecorderLocked() error {
	if f.recordPath == "" || f.recorder != nil {
		return nil
	}
	file, err := os.OpenFile(f.recordPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("faux: open record file: %w", err)
	}
	f.recorder = file
	return nil
}

func (f *Faux) recordLocked(req Request) error {
	if f.recorder == nil {
		return nil
	}
	line, err := json.Marshal(fauxRecord{
		Model:    req.Model,
		Messages: Wire(req.Messages),
		Stream:   true,
	})
	if err != nil {
		return fmt.Errorf("faux: encode record: %w", err)
	}
	if _, err := f.recorder.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("faux: write record: %w", err)
	}
	return nil
}
