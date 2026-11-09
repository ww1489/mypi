// Package ai 定义 agent 循环所需的 provider 契约、消息类型与流式事件。
//
// 命名刻意与 PiG 的 ai 包保持一致，方便两棵代码树并排阅读。这里只是 mypi
// 当前步骤需要的最小子集；后续每一步都会把它加宽，而"加宽"本身就是教学点。
package ai

import "context"

// ContentBlock 是消息内容的一个片段。
//
// 第 01 步只会产出文本。工具调用会在后续步骤作为新的块类型出现，所以这里保留
// Type 判别字段，而不是退化成裸字符串。
type ContentBlock struct {
	Type string `json:"type"` // "text"
	Text string `json:"text,omitempty"`
}

// Text 构造只含一个文本块的切片。
func Text(s string) []ContentBlock {
	return []ContentBlock{{Type: "text", Text: s}}
}

// Message 是 agent 与 provider 之间交换的角色总和。
//
// Go 没有求和类型，所以用"接口 + 具体类型 + 边界处的类型分支"来表达这组并集。
// 字段名与 JSON tag 镜像 PiG，方便后续步骤逐字段对照会话文件。
type Message interface {
	Role() string
}

// UserMessage 是人类发出的消息。
type UserMessage struct {
	Content []ContentBlock `json:"content"`
}

// Role 实现 Message 接口。
func (m *UserMessage) Role() string { return "user" }

// AssistantMessage 是模型发出的消息。
type AssistantMessage struct {
	Content    []ContentBlock `json:"content"`
	Provider   string         `json:"provider,omitempty"`
	Model      string         `json:"model,omitempty"`
	StopReason string         `json:"stopReason,omitempty"`
}

// Role 实现 Message 接口。
func (m *AssistantMessage) Role() string { return "assistant" }

// provider 返回的停止原因。
const (
	StopReasonStop  = "stop"
	StopReasonError = "error"
)

// TextOf 拼接所有文本块，忽略其它块类型。
func TextOf(blocks []ContentBlock) string {
	var out string
	for _, b := range blocks {
		if b.Type == "text" {
			out += b.Text
		}
	}
	return out
}

// WireMessage 是 provider 在线路上看到的 JSON 形状。
//
// 之所以单独存在：Go 消息类型把角色隐含在具体类型里，而协议需要它作为一个
// 字段。录制文件复用同一形状，因此测试可以断言"模型究竟被告知了什么"。
type WireMessage struct {
	Role    string         `json:"role"`
	Content []ContentBlock `json:"content"`
}

// Wire 把消息转换成线路表示。
func Wire(msgs []Message) []WireMessage {
	out := make([]WireMessage, 0, len(msgs))
	for _, m := range msgs {
		switch v := m.(type) {
		case *UserMessage:
			out = append(out, WireMessage{Role: v.Role(), Content: v.Content})
		case *AssistantMessage:
			out = append(out, WireMessage{Role: v.Role(), Content: v.Content})
		}
	}
	return out
}

// Request 是一次 provider 调用。
type Request struct {
	Model    string
	System   string
	Messages []Message
}

// Event 是 provider 流式输出中的一项。
type Event interface{ isEvent() }

// TextDeltaEvent 携带一段增量助手文本。
type TextDeltaEvent struct{ Text string }

// MessageDoneEvent 携带完整的助手消息。
type MessageDoneEvent struct{ Message *AssistantMessage }

// ErrorEvent 表示流式输出失败。
type ErrorEvent struct{ Err error }

func (TextDeltaEvent) isEvent()   {}
func (MessageDoneEvent) isEvent() {}
func (ErrorEvent) isEvent()       {}

// Provider 是每个模型后端都要实现的接口。
type Provider interface {
	// ID 标识后端，例如 "faux"。
	ID() string
	// Stream 发起一次响应。返回的 channel 由 provider 负责关闭，且必定以
	// MessageDoneEvent 或 ErrorEvent 收尾。
	Stream(ctx context.Context, req Request) (<-chan Event, error)
}
