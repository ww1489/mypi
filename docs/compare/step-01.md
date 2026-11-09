# step-01 与 PiG 的差异清单

对应提交：`step-01`（骨架 + 假 provider）。

## 文件对应

| mypi | PiG | 说明 |
| --- | --- | --- |
| `ai/types.go` | `ai/types.go` + `ai/messages.go` | 消息与内容块。PiG 分成两个文件，我们合成一个 |
| `ai/faux.go` | `ai/faux.go` | 假 provider。我们的多了"每请求写一行 JSON"的录制 |
| `agent/agent.go` | `agent/standalone_loop.go` | 发起一次请求。PiG 的 145 行骨架我们只取了最外层 |
| `cmd/mypi/cli.go` | `coding/cli/args.go` + `coding/cli/main.go` | 手写参数解析 |
| `e2e/cli_test.go` | `PiG/test/` | 进程级黑盒测试 |

## 已经照着做的

1. **provider 接口的形状**：`ID() string` + `Stream(ctx, req) (<-chan Event, error)`。PiG 还有 `io.Closer`，我们用类型断言按需处理（`ai/faux.go` 的 `Close`）。
2. **事件是封闭并集**：用私有方法 `isEvent()` 关住，只有本包能实现。PiG 的 `AgentEvent` 也是这个套路。
3. **契约写在注释里，而不是靠约定**：channel 由实现方关闭、最后一条一定是 done 或 error。这条契约到 step-04 换成真 SSE 时不用改。
4. **消息的 JSON 字段名**：`role`、`content`、`toolCallId`、`stopReason`、`isError`。后面存会话文件时可以和 PiG 逐字段对比。

## 故意不一样的地方

### 1. ContentBlock 是个扁平结构体，不是接口

我们的写法：

```go
type ContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}
```

PiG 的写法（`PiG/ai/types.go:367`）：

```go
type ContentBlock interface {
	contentBlock()
	contentType() string
}
```

它有四个实现：`TextContent`、`ImageContent`、`ToolCall`、`ThinkingContent`。而且 `type` 字段不是结构体字段，是 `MarshalJSON` 里拼出来的（`PiG/ai/types.go:273`）。

PiG 还按角色限制了能放哪些块（`PiG/ai/messages.go:51-104`）：`UserContentBlock` 只允许 text/image，`AssistantContentBlock` 只允许 text/thinking/toolCall。同一个 `TextContent` 实现了三种"允许"接口，所以"assistant 消息里塞图片"在编译期就报错。

**为什么现在不这么做**：现在只有一种块。改成 PiG 的形状要新增 4 个类型、自定义序列化、角色接口，换来的收益（编译期挡住非法组合）此刻不存在——因为还没有第二种块可以组合错。

**代价是什么**：编译器不拦 `ContentBlock{Type: "toolCall", Text: "x"}`，也不拦 `Type: "image"` 却没有 `Data`。现在不会出问题，加了工具调用之后会。

**什么时候改**：票 03（`03-read-tool-loop.md`）。那时第一次出现"assistant 内容里有两种块并且要分别处理"，接口方案的好处才看得见，切换成本也最低（此时只被 `Text`、`Wire`、`faux` 和一处测试用到）。

### 2. Message 接口多了一个 `Blocks()`

PiG 的 `Message`（`PiG/ai/messages.go:13`）是 `messageRole()` + `MessageRole()` + `cloneMessage()`，读内容靠断言到具体类型再取 `.Content`。

我们加了 `Blocks() []ContentBlock`，原因是 `Wire()` 里两个分支代码完全一样，说明判断多余。副作用是以后新增消息类型（比如票 03 的 `ToolResultMessage`）只要实现 `Blocks()` 就会自动被发出去，不会因为漏写 `case` 被静默丢掉。

PiG 不需要这个，因为它本来就要求按类型分情况处理内容。

### 3. 没有 cloneMessage

PiG 每个消息都带 `cloneMessage()`，因为会话树分叉时要做深拷贝（票 09 的事）。我们现在还没有会话树，所以没有。

### 4. 消息字段少很多

`AssistantMessage` 在 PiG 里还有 `API`、`ResponseModel`、`ResponseID`、`Usage`、`RawStopReason`、`Diagnostics` 等。我们只有 `Content`、`Provider`、`Model`、`StopReason`。缺的那些等用到时再加：
- `Usage`（token 计数）→ 票 10 做压缩时必须要
- `API` → 票 04 接真 provider 时可能要，用于区分协议

### 5. 没有 system prompt

`Request.System` 字段有了，但 CLI 从来不填。PiG 的 system prompt 是一整套分节拼装（`internal/codingagent/prompts/coding.go`）。我们到票 03 引入工具声明增量时才会第一次真正需要它。

### 6. system 是 Request 上的一个字段，不是消息数组里的元素

我们是 `Request{Model, System string, Messages []Message}`。

PiG 把系统提示当成 `Messages` 数组里的一条 `SystemMessage`（`PiG/ai/messages.go:107`），它本身还带 `Sections`、`ToolsAdded`、`ToolsRemoved`：

```go
type SystemMessage struct {
	Content      SystemContent   `json:"content"`
	Sections     OrderedSections `json:"sections,omitempty"`
	Timestamp    int64           `json:"timestamp"`
	ToolsAdded   []ToolSchema    `json:"toolsAdded,omitempty"`
	ToolsRemoved []ToolReference `json:"toolsRemoved,omitempty"`
}
```

**为什么要跟着改**：工具声明增量是挂在 system 消息上的，而且可能在对话中间出现（比如第 3 轮才加了一个工具）。如果 system 只能是一个固定的开头字符串，增量就无处安放。

**已知的坑**：`Request.System` 目前没有任何代码把它发出去——`Wire()` 只遍历 `Messages`。也就是说现在接真 provider 的话，系统提示会被静默丢掉，而且不报错。测试里录制到的 `"system":""` 会让人误以为"有这个字段就是生效的"。票 03 把 system 挪进数组后，这个问题会自动消失。

**什么时候改**：票 03，和"工具声明以 system 消息增量下发"一起做。

## 下一票的前置

- 票 02 只需要 `Agent.Run` 外面套一层输入循环，`ai` 包不用动。
- 票 03 会同时做三件事：加 `ToolResultMessage` + 把 `ContentBlock` 升级成接口 + 多实现 + 把 system 提示挪进 Messages 数组（带工具声明增量）。
