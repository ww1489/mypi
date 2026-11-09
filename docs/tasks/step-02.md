# 票 02 任务卡：多轮对话循环

## 这一步要回答的问题

**每轮为什么要重发整段历史？历史该由谁持有？**

模型本身不记事，它看到的只有这次请求里的 `Messages`。所以"记得前面说过什么"这件事，完全是客户端把历史攒起来再发一遍的功劳。想清楚这一点，再决定历史放在哪：放进 `Agent`（有状态），还是留在调用方（无状态）。

## 你要做的

1. 不带参数启动时进入**交互模式**：从 stdin 一行一行读问题，每读一行就算一轮。
2. 每一轮把「之前所有消息 + 这一句」发给 provider，再把回复追加进历史。
3. 单次提问（带参数）和交互模式**必须走同一个** `Agent.Run` 调用路径，不能各写一套。
4. 输入结束（EOF / Ctrl-D）时退出码 0，不打印堆栈。
5. 空行跳过，不发起请求。
6. 某一轮 provider 报错：错误打到 stderr，循环继续；但**历史里要留下一条失败记录**——追加一条 `StopReason=error` 的 assistant 消息（带你拿到的错误文案）。
7. 如果 stdin 是终端，打印 `> ` 提示符；从管道读的时候不要掺提示符，否则会污染输出。

## 约束

- **不要动 `ai` 包**。这一票只改 `cmd/mypi`（必要时加 `agent`），协议层留到票 03。
- `agent.Agent.Run(ctx, history) ([]ai.Message, error)` 的签名保持不变。
- 不引第三方依赖；判断是不是终端用标准库就够。
- 注释用中文、说人话；类型声明逐行展开；函数体不写单行。

## 验收测试在哪

测试我写好了，放在分支 `step-02-solution` 上（**不是**让你抄实现，那个分支上有实现，先别看）。取测试到工作区，不要提交：

```bash
git checkout step-02-solution -- e2e/multi_turn_test.go e2e/cli_test.go
```

`cli_test.go` 也要取，因为多轮测试用到了里面新增的 `runWithInput`，而且原来那条「缺 prompt 报错」的测试在这一票里变成了「无输入干净退出」——这是本票的预期行为变更。

取完跑测试，应该是**红的**。你的任务就是把它变绿。

## 验收命令

```bash
go vet ./... && go test ./... -count=1
printf '第一问\n第二问\n' | go run ./cmd/mypi
```

第二条命令要能看到两段回答，并且第二次请求带着第一轮的一问一答（加 `MYPY_FAUX_RECORD=/tmp/rec.jsonl` 看录制文件）。

## PiG 是怎么做的（已替你查过）

### 失败的轮次：进历史

`PiG/agent/agent_loop.go:293-311`：`runTurn` 里 `appendAssistant` 在校验 StopReason **之前**：

```go
assistant, toolCalls, err := r.streamAssistantResponse()
r.appendAssistant(assistant)
if assistant.StopReason == ai.StopReasonError || assistant.StopReason == ai.StopReasonAborted {
    ...  // 硬退出
}
```

出错时它会合成一条空文本的 assistant 消息，带 `StopReason: error` 和 `ErrorMessage`（`agent_loop.go:553-585`）。于是失败后的历史是 `[user, assistant(error)]`——**结构完整**，不会出现"用户问了但没人回答"的断头记录。

对我们的要求：`AssistantMessage` 加一个 `ErrorMessage` 字段；`Agent.Run` 失败时返回「历史 + 错误消息」以及错误本身；调用方收下这份历史再继续。

### 终端 vs 管道：决定的是模式，不只是提示符

`PiG/coding/cli/main.go:203`：

```go
if term.IsTerminal(int(os.Stdin.Fd())) {
    return "", nil   // 是终端 → 不读管道
}
```

stdin 是终端就进交互模式；不是终端就把 stdin 整个读成输入走 print 模式。再加 `print_mode.go:179`：stdout 不是终端时接管 stdout，把杂项输出改道 stderr，保证输出里只有答案。

### 切片：谁拥有谁维护，递出去时复制

- 自己攒历史：`appendMessage` 直接 `r.context = append(...)`（`agent_loop.go:408`）
- 交给外部：`turnContext` 里 `slices.Clone(r.context)`、`slices.Clone(r.newMessages)`（`agent_loop.go:337`）
- 事件里带消息：`startEventMessage` 把指针字段复制一份（`agent_loop.go:418`）

所以 `append(clone(history), ...)` 这种写法是对的：先复制再拼，绝不改调用方那份。

## 实在卡住

参考实现在分支 `step-02-solution`（`git diff 67ac58a..step-02-solution -- cmd/mypi`）。超过两小时再看，按 `AGENTS.md` 记 `step-02-ref`，并补一份「我卡在哪」的笔记放进 `docs/notes/`。
