// Package ai defines the provider contract, message types, and streaming events
// the agent loop speaks.
//
// Names mirror PiG's ai package so the two trees can be read side by side. The
// shapes here are the deliberately small subset mypi needs at this step; each
// later step widens them, and the widening is the lesson.
package ai

import "context"

// ContentBlock is one piece of message content.
//
// Step 01 only produces text. Tool calls arrive in later steps as additional
// block types, which is why the block carries a Type discriminator rather than
// being a bare string.
type ContentBlock struct {
	Type string `json:"type"` // "text"
	Text string `json:"text,omitempty"`
}

// Text builds a single text block list.
func Text(s string) []ContentBlock {
	return []ContentBlock{{Type: "text", Text: s}}
}

// Message is the sum of the roles the agent exchanges with a provider.
//
// Go has no sum types, so the union is expressed as an interface plus concrete
// types and type switches at the edges. Field names and JSON tags mirror PiG so
// that a later step can compare session files field by field.
type Message interface {
	Role() string
}

// UserMessage is something the human said.
type UserMessage struct {
	Content []ContentBlock `json:"content"`
}

// Role implements Message.
func (m *UserMessage) Role() string { return "user" }

// AssistantMessage is something the model said.
type AssistantMessage struct {
	Content    []ContentBlock `json:"content"`
	Provider   string         `json:"provider,omitempty"`
	Model      string         `json:"model,omitempty"`
	StopReason string         `json:"stopReason,omitempty"`
}

// Role implements Message.
func (m *AssistantMessage) Role() string { return "assistant" }

// StopReason values returned by providers.
const (
	StopReasonStop  = "stop"
	StopReasonError = "error"
)

// TextOf concatenates every text block, ignoring other block types.
func TextOf(blocks []ContentBlock) string {
	var out string
	for _, b := range blocks {
		if b.Type == "text" {
			out += b.Text
		}
	}
	return out
}

// WireMessage is the JSON shape a provider sees on the wire.
//
// It exists because the Go message types keep the role implicit in the concrete
// type, while the protocol needs it as a field. Recordings use the same shape,
// so a test can assert exactly what the model was told.
type WireMessage struct {
	Role    string         `json:"role"`
	Content []ContentBlock `json:"content"`
}

// Wire converts messages into their wire representation.
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

// Request is one provider call.
type Request struct {
	Model    string
	System   string
	Messages []Message
}

// Event is one item in a provider's streaming output.
type Event interface{ isEvent() }

// TextDeltaEvent carries an incremental slice of assistant text.
type TextDeltaEvent struct{ Text string }

// MessageDoneEvent carries the completed assistant message.
type MessageDoneEvent struct{ Message *AssistantMessage }

// ErrorEvent reports that the stream failed.
type ErrorEvent struct{ Err error }

func (TextDeltaEvent) isEvent()   {}
func (MessageDoneEvent) isEvent() {}
func (ErrorEvent) isEvent()       {}

// Provider is the interface each model backend implements.
type Provider interface {
	// ID identifies the backend, e.g. "faux".
	ID() string
	// Stream starts one response. The returned channel is closed by the
	// provider; it always ends with either MessageDoneEvent or ErrorEvent.
	Stream(ctx context.Context, req Request) (<-chan Event, error)
}
