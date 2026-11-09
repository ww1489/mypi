// Package agent 负责调用 provider，并把结果拼回对话历史。
//
// 现在一次只发一个请求。多轮对话、工具调用、事件分发都在后面的步骤里加，
// 这样每次新增都能在 diff 里看得很清楚。
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
			// 失败也要留下记录：追加一条错误消息再返回错误。
			// 这样历史仍是「用户问了、模型答了（答的是错误）」，结构不会断。
			return a.withFailure(history, e.Err.Error()), e.Err
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

// withFailure 在历史末尾补一条错误消息。
func (a *Agent) withFailure(history []ai.Message, reason string) []ai.Message {
	out := make([]ai.Message, 0, len(history)+1)
	out = append(out, history...)
	out = append(out, &ai.AssistantMessage{
		Content:      ai.Text(""),
		Provider:     a.Provider.ID(),
		Model:        a.Model,
		StopReason:   ai.StopReasonError,
		ErrorMessage: reason,
	})
	return out
}
