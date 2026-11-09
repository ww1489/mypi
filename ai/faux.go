package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
)

// 配置 faux provider 的环境变量。
const (
	// FauxScriptEnv 指向被回放的 JSON 脚本。
	FauxScriptEnv = "MYPY_FAUX_SCRIPT"
	// FauxRecordEnv 指向一个文件，每个请求向其追加一行 JSON。
	// 它就是测试使用的观测面。
	FauxRecordEnv = "MYPY_FAUX_RECORD"
)

// FauxScript 是磁盘上的回放脚本。
//
//	{"responses":[{"chunks":["hello"," world"],"stopReason":"stop"}]}
//
// 响应按顺序消费，每个请求取一条。带 "error" 字段的响应会让该请求失败，
// 而不是产出文本。
type FauxScript struct {
	Responses []FauxResponse `json:"responses"`
}

// FauxResponse 是一条脚本化的回复。
type FauxResponse struct {
	Chunks     []string `json:"chunks"`
	StopReason string   `json:"stopReason,omitempty"`
	Error      string   `json:"error,omitempty"`
}

// Faux 是一个脚本化 provider：它回放预置的假回复，而不会调用模型。每个请求
// 都会追加到录制文件，使测试能够断言 agent 实际发送了什么，而不只是打印了
// 什么。
//
// 构造时不读取任何文件，因此 --help、--version 在无配置的环境下也能正常工作。
type Faux struct {
	scriptPath string
	recordPath string

	mu       sync.Mutex
	script   *FauxScript
	cursor   int
	recorder *os.File
}

// NewFaux 依据环境变量构造 provider。
func NewFaux() *Faux {
	return &Faux{
		scriptPath: os.Getenv(FauxScriptEnv),
		recordPath: os.Getenv(FauxRecordEnv),
	}
}

// ID 实现 Provider 接口。
func (f *Faux) ID() string {
	return "faux"
}

// Close 释放录制文件。
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

// Stream 实现 Provider 接口。
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
	if f.scriptPath == "" {
		return fmt.Errorf("faux: %s is not set", FauxScriptEnv)
	}
	raw, err := os.ReadFile(f.scriptPath)
	if err != nil {
		return fmt.Errorf("faux: read script: %w", err)
	}
	var script FauxScript
	if err := json.Unmarshal(raw, &script); err != nil {
		return fmt.Errorf("faux: parse script %s: %w", f.scriptPath, err)
	}
	f.script = &script
	return nil
}

// fauxRecord 是每个请求写下的稳定、可 diff 的形状。
type fauxRecord struct {
	Model    string        `json:"model"`
	System   string        `json:"system"`
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
		System:   req.System,
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
