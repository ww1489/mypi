# 02: 多轮对话循环

**What to build:** 一次进程内可以连续多轮对话：交互式 REPL 与单次提问共用同一条循环，历史按顺序累积并随每次请求完整发出。第二轮请求里必须能看到第一轮的完整消息（用户消息与助手回复），证明上下文确实是累积的而不是各自独立提问。输入结束（EOF / Ctrl-D）时干净退出，不打印堆栈。

**Blocked by:** 01

**Status:** done

- [x] 同一次运行中连续三轮提问，第三轮请求的录制载荷包含前两轮的全部消息且顺序正确
- [x] 单次提问模式与 REPL 模式共用同一循环实现（行为一致，仅输入来源不同）
- [x] 关闭输入（EOF / Ctrl-D）后进程以退出码 0 结束且无 panic
- [x] 消息的角色与内容结构可被解析回等价的内部表示（往返一致）
- [x] 既有测试仍然全绿（例外：`TestMissingPromptIsUsageError` 因"无参数进入交互模式"改写为 `TestNoInputAndNoPromptExitsCleanly`，属本票的预期行为变更）

## 完成记录

- 实现：`cmd/mypi/cli.go` 拆出 `askOnce` / `askRepeatedly`，两者都走同一个 `agent.Agent.Run`
- 测试：`e2e/multi_turn_test.go`（历史增长、答案顺序、空行忽略、失败不污染历史、EOF 干净退出）
- 内置脚本扩到三条回复，交互模式克隆仓库就能连问三轮

## 验收命令

```bash
printf '第一问\n第二问\n' | go run ./cmd/mypi
go test ./e2e -run Repl -count=1
```
