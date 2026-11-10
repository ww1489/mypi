// Package ai 放 agent 循环要用到的东西：provider 接口、消息类型、流式事件。
//
// 名字和 PiG 的 ai 包保持一致，方便两边对着看。这里只有当前步骤用得上的部分，
// 后面的步骤会一点点往里加。
package ai

import "context"

// ContentBlock 是消息内容的一小块。
//
// 现在只有文本。以后要加工具调用，所以留了 Type 字段，而不是直接存一段字符串。
type ContentBlock struct {
	Type string `json:"type"` // "text"
	Text string `json:"text,omitempty"`
}

// Text 把一段字符串包成内容块。
func Text(s string) []ContentBlock {
	return []ContentBlock{{Type: "text", Text: s}}
}

// Message 是对话里的一条消息，要么是用户说的，要么是模型说的。
//
// Go 没有"这几个类型之一"的写法，所以用接口表示，要分情况处理时再 switch。
// 字段名和 JSON tag 跟 PiG 一样，以后存会话文件可以直接对比。
type Message interface {
	Role() string
	// Blocks 返回这条消息的内容。有了它，不管来的是哪种消息，
	// 都能统一读内容，不用每加一种消息就改一遍转换代码。
	Blocks() []ContentBlock
}

// UserMessage 是用户说的话。
type UserMessage struct {
	Content []ContentBlock `json:"content"`
}

// Role 返回角色名。
func (m *UserMessage) Role() string {
	return "user"
}

// Blocks 返回消息内容。
func (m *UserMessage) Blocks() []ContentBlock {
	return m.Content
}

// AssistantMessage 是模型返回的内容。
type AssistantMessage struct {
	Content      []ContentBlock `json:"content"`
	Provider     string         `json:"provider,omitempty"`
	Model        string         `json:"model,omitempty"`
	StopReason   string         `json:"stopReason,omitempty"`
	ErrorMessage string         `json:"errorMessage,omitempty"`
}

// Role 返回角色名。
func (m *AssistantMessage) Role() string {
	return "assistant"
}

// Blocks 返回消息内容。
func (m *AssistantMessage) Blocks() []ContentBlock {
	return m.Content
}

// 模型停下来时的原因。
const (
	StopReasonStop  = "stop"  // 正常说完了
	StopReasonError = "error" // 出错了
)

// TextOf 把所有文本块拼起来，其他类型的块跳过。
func TextOf(blocks []ContentBlock) string {
	var out string
	for _, b := range blocks {
		if b.Type == "text" {
			out += b.Text
		}
	}
	return out
}

// WireMessage 是真正发给 provider 的 JSON 结构。
//
// Go 这边靠具体类型区分角色，但协议里角色是一个字段，所以发请求前要转一下。
// 请求录制也用这个结构，测试就能直接看到"模型到底收到了什么"。
type WireMessage struct {
	Role    string         `json:"role"`
	Content []ContentBlock `json:"content"`
}

// Wire 把消息转成能发出去的 JSON 结构。
func Wire(msgs []Message) []WireMessage {
	out := make([]WireMessage, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, WireMessage{
			Role:    m.Role(),
			Content: m.Blocks(),
		})
	}
	return out
}

// Request 是一次请求。
//
// Messages 是整段对话，不是一条消息。模型不记事，每次请求都得把全部历史重发。
// 单次问答时它只有一条，多轮对话和工具调用之后会越来越长。
//
// 这里还没有系统提示。等票 03 引入工具声明时，会按 PiG 的做法加一条 system
// 消息进 Messages，而不是在 Request 上挂一个字段。
type Request struct {
	Model    string
	Messages []Message
}

// Event 是流式输出里的一条事件。
type Event interface {
	isEvent()
}

// TextDeltaEvent 是一小段刚生成的回答文本。
type TextDeltaEvent struct {
	Text string
}

// DoneEvent 表示这次回答结束了，并且带着完整的消息。
type DoneEvent struct {
	Message *AssistantMessage
}

// ErrorEvent 表示这次流式输出失败了。
type ErrorEvent struct {
	Err error
}

// 下面三个方法让 Event 只能由本包里的类型实现。
func (TextDeltaEvent) isEvent() {}
func (DoneEvent) isEvent()      {}
func (ErrorEvent) isEvent()     {}

// Provider 是每个模型后端都要实现的接口。
type Provider interface {
	// ID 返回后端名字，比如 "faux"。
	ID() string

	// Stream 发一次请求，用一个 channel 把结果流式吐出来。
	// channel 由实现方关闭，最后一条一定是 DoneEvent 或 ErrorEvent。
	Stream(ctx context.Context, req Request) (<-chan Event, error)
}
