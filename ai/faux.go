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

// Environment variables that configure the faux provider.
const (
	// FauxScriptEnv points at the JSON script replayed by the provider.
	FauxScriptEnv = "MYPY_FAUX_SCRIPT"
	// FauxRecordEnv points at a file that receives one JSON line per request.
	// It is the observation surface used by tests.
	FauxRecordEnv = "MYPY_FAUX_RECORD"
)

// FauxScript is the on-disk replay script.
//
//	{"responses":[{"chunks":["hello"," world"],"stopReason":"stop"}]}
//
// Responses are consumed in order, one per request. A response with "error"
// set fails that request instead of producing text.
type FauxScript struct {
	Responses []FauxResponse `json:"responses"`
}

// FauxResponse is one scripted reply.
type FauxResponse struct {
	Chunks     []string `json:"chunks"`
	StopReason string   `json:"stopReason,omitempty"`
	Error      string   `json:"error,omitempty"`
}

// Faux is a scripted provider: it replays canned responses instead of calling a
// model. Every request is appended to a recording file so tests can assert what
// the agent actually sent, not just what it printed.
//
// Nothing is read at construction time, so commands such as --help and --version
// work with no configuration present.
type Faux struct {
	scriptPath string
	recordPath string

	mu       sync.Mutex
	script   *FauxScript
	cursor   int
	recorder *os.File
}

// NewFaux builds a provider from the environment.
func NewFaux() *Faux {
	return &Faux{
		scriptPath: os.Getenv(FauxScriptEnv),
		recordPath: os.Getenv(FauxRecordEnv),
	}
}

// ID implements Provider.
func (f *Faux) ID() string { return "faux" }

// Close releases the recording file.
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

// Stream implements Provider.
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

// fauxRecord is the stable, diffable shape written per request.
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
