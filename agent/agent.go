// Package agent 负责调用 provider，并把结果拼回对话历史。
//
// 这里一次只发一个请求。调用方可以反复调用 Run，把上一轮返回的历史带进下一轮；
// 工具调用和事件分发在后面的步骤里加。
package agent

import (
	"context"
	"errors"
	"strings"

	"mypi/ai"
)

// Agent 拿着对话历史去问 provider。
type Agent struct {
	Provider ai.Provider
	Model    string
	// OnEvent 每来一个流式事件就调一次。不需要就留空。
	OnEvent func(ai.Event)
}

// Run 发一次请求，返回「原来的历史 + 模型这条回复」。
// 传进来的切片不会被改动。
func (a *Agent) Run(ctx context.Context, history []ai.Message) ([]ai.Message, error) {
	if a.Provider == nil {
		return history, errors.New("agent: no provider configured")
	}

	stream, err := a.Provider.Stream(ctx, ai.Request{
		Model:    a.Model,
		Messages: history,
	})
	if err != nil {
		return a.failedTurn(history, err)
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
		case ai.DoneEvent:
			done = e.Message
		case ai.ErrorEvent:
			return a.failedTurn(history, e.Err)
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

// failedTurn 给失败的一轮也补一条 assistant 消息。
// 这样下一轮看到的仍然是「问一句、答一句」的完整顺序。
func (a *Agent) failedTurn(history []ai.Message, err error) ([]ai.Message, error) {
	failed := &ai.AssistantMessage{
		Provider:     a.Provider.ID(),
		Model:        a.Model,
		StopReason:   ai.StopReasonError,
		ErrorMessage: err.Error(),
	}
	out := make([]ai.Message, 0, len(history)+1)
	out = append(out, history...)
	out = append(out, failed)
	return out, err
}
