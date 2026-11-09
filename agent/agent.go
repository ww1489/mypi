// Package agent holds the loop that turns provider events into conversation
// history.
//
// Step 01 only performs a single request. The multi-turn loop, tool calls, and
// event fan-out arrive in later steps; keeping this file small now makes each
// of those additions legible as a diff.
package agent

import (
	"context"
	"errors"
	"strings"

	"mypi/ai"
)

// Agent drives one provider over a conversation history.
type Agent struct {
	Provider ai.Provider
	Model    string
	System   string
	// OnEvent observes every streamed event. It may be nil.
	OnEvent func(ai.Event)
}

// Run sends the history to the provider and returns the history with the
// assistant reply appended. The input slice is never mutated.
func (a *Agent) Run(ctx context.Context, history []ai.Message) ([]ai.Message, error) {
	if a.Provider == nil {
		return history, errors.New("agent: no provider configured")
	}

	stream, err := a.Provider.Stream(ctx, ai.Request{
		Model:    a.Model,
		System:   a.System,
		Messages: history,
	})
	if err != nil {
		return history, err
	}

	var text strings.Builder
	var done *ai.AssistantMessage
	for ev := range stream {
		if a.OnEvent != nil {
			a.OnEvent(ev)
		}
		switch e := ev.(type) {
		case ai.TextDeltaEvent:
			text.WriteString(e.Text)
		case ai.MessageDoneEvent:
			done = e.Message
		case ai.ErrorEvent:
			return history, e.Err
		}
	}

	if done == nil {
		done = &ai.AssistantMessage{
			Content:    ai.Text(text.String()),
			Provider:   a.Provider.ID(),
			Model:      a.Model,
			StopReason: ai.StopReasonStop,
		}
	}

	out := make([]ai.Message, 0, len(history)+1)
	out = append(out, history...)
	out = append(out, done)
	return out, nil
}
