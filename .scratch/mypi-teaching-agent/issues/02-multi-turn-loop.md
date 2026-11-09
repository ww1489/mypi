# 02: 多轮对话循环

**What to build:** 一次进程内可以连续多轮对话：交互式 REPL 与单次提问共用同一条 `Agent.Run` 路径，历史按顺序累积并随每次请求完整发出。第三轮请求里必须能看到前两轮的全部消息，证明上下文确实在累积。输入结束（EOF / Ctrl-D）时干净退出，不打印堆栈。某一轮 provider 失败时，错误打到 stderr、循环继续，历史里留下一条失败记录。

**Blocked by:** 01

**Status:** ready-for-agent

- [ ] 无参数启动且输入立即结束：退出码 0，stdout/stderr 都没有输出，不 panic
- [ ] 连续三轮提问：第三次请求的录制载荷是 5 条消息，角色 user/assistant 交替
- [ ] 每轮答案按输入顺序输出，相邻两轮之间只有一个换行（`first\nsecond\n`）
- [ ] 空行与纯空白行不发起请求
- [ ] 某轮 provider 报错：stderr 有错误、循环继续，下一轮请求里保留失败那条 assistant 消息
- [ ] 单次提问与 REPL 共用同一个 `Agent.Run`，两条路径都能跑通
- [ ] 既有测试仍全绿（唯一预期变更：`TestMissingPromptIsUsageError` 因"无参数进入交互"改写为 `TestNoInputAndNoPromptExitsCleanly`）

## 验收测试

| 测试 | 对应验收项 |
| --- | --- |
| `TestNoInputAndNoPromptExitsCleanly` | 第 1 条 |
| `TestReplEOFExitsCleanly` | 第 1 条 |
| `TestReplSendsGrowingHistory` | 第 2 条、第 6 条 |
| `TestReplPrintsEveryAnswer` | 第 3 条 |
| `TestReplIgnoresBlankLines` | 第 4 条 |
| `TestReplRecordsFailedTurnInHistory` | 第 5 条 |

取测试：`git checkout step-02-solution -- e2e/multi_turn_test.go e2e/cli_test.go`（测试文件不单独提交，跟实现一起进本票的 commit）。
