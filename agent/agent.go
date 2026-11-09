// Package agent 承载把 provider 事件变成对话历史的循环。
//
// 第 01 步只做一次请求。多轮循环、工具调用与事件分发会在后续步骤加入；
// 现在让这个文件足够小，后续每次增加才能作为 diff 被看清楚。
package agent

import (
	"context"
	"errors"
	"strings"

	"mypi/ai"
)

// Agent 在已有对话历史上驱动一个 provider。
type Agent struct {
	Provider ai.Provider
	Model    string
	System   string
	// OnEvent 观察每一个流式事件，可以为 nil。
	OnEvent func(ai.Event)
}

// Run 把历史发送给 provider，并返回追加了助手回复的新历史。
// 入参切片不会被原地修改。
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
