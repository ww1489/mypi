# 票 02 任务卡：多轮对话循环

## 1. 目标

**让 `mypi` 能连续对话**：不带参数启动时，从 stdin 一行一行读问题，每读一行问一次模型，并且每次都把之前所有消息一起发过去。

一句话验收：

```bash
printf '第一问\n第二问\n' | go run ./cmd/mypi
```

要看到两段回答，且录制文件里第二次请求包含「第一问 + 第一答 + 第二问」。

**为什么每轮要重发**：模型不记事。它看到的只有这次请求的 `Messages`，"记得前面说过什么"完全是客户端攒历史再重发的功劳。

## 2. 起点：当前代码的三个事实

先认清现状，任务就是从这三条推出来的：

| 现状 | 位置 | 导致的问题 |
| --- | --- | --- |
| `run(args, stdout, stderr)` 拿不到 stdin | `cmd/mypi/cli.go` | 没法读交互输入 |
| 没给 prompt 就 `exitUsage` | `cmd/mypi/cli.go` | 无参数直接失败 |
| `Run` 失败时 `return history, err`，历史里没有失败痕迹 | `agent/agent.go` | 出错那轮像没发生过 |

## 3. 任务拆解

### T1 · 让 `run` 拿到 stdin

> 对应验收项：无——它是后面所有任务的实现前提。

**做什么**：给 `run` 加一个 `io.Reader` 参数，`main.go` 传 `os.Stdin`。

**提示**：参数放在 `args` 之后、`stdout` 之前，读起来最顺。

**完成标志**：`go build ./...` 通过。

---

### T2 · 按有没有 prompt 分流

> 对应验收项：第 1 条（无参数 + 输入结束干净退出）、第 6 条（两条路径共用同一个 `Agent.Run`）。

**做什么**：删掉"缺 prompt → `exitUsage`"那段，改成两个分支——有 prompt 走单次，没有走交互。两条路必须共用同一个 `agent.Agent` 实例。

**提示**：删掉的是**整个 if 块**，不是把条件改宽。分流点放在 `ag` 构造之后，这样 provider、`OnEvent`、`ctx` 都只有一份。

**完成标志**：`go run ./cmd/mypi` 不再报 `missing prompt`，而是等待输入（Ctrl-D 退出）。

---

### T3 · 交互循环

> 对应验收项：第 1 条（EOF 退出码 0）、第 2 条（历史累积、角色交替）、第 4 条（空行不发请求）。

**做什么**：读一行 → 跳过空行 → 拼「历史 + 这一句」→ 调 `Run` → 打印 → 继续；读到 EOF 退出，退出码 0。

**提示**：

- `bufio.Scanner` 默认单行上限 64KB，粘长文本会报错，先用 `scanner.Buffer(...)` 放宽
- 历史要**跨轮保留在循环外**，不是每轮新建
- 拼历史时别原地 `append` 调用方切片（见 T4 提示）
- 读失败/读到结尾都要能正常结束，不要 `return` 一个非零码

**完成标志**：三行输入 → 三次请求，第三次请求里有 5 条消息，角色是 user/assistant 交替。

---

### T4 · 失败留痕

> 对应验收项：第 5 条（报错后循环继续，历史里保留失败那条 assistant 消息）。

**做什么**：`Agent.Run` 失败时，返回「历史 + 一条 `StopReason: error` 的 assistant 消息」**并且**返回错误；`AssistantMessage` 加 `ErrorMessage string` 字段；CLI 失败时把这份历史收下再继续。

**提示**：

- 这是本票唯一允许改 `ai` 包的地方（加一个字段）
- 别原地 `append(history, ...)`：调用方可能还在用那份切片。先 `append(make([]ai.Message, 0, len(history)+1), history...)` 复制一份再拼。PiG 也是这个规矩（`turnContext` 里 `slices.Clone`）
- CLI 那边顺序很重要：**先收下返回的历史，再判断 err**

**完成标志**：失败一轮后，下一轮请求里是 `[user, assistant, user]` 三条。

---

### T5 · 输出格式与提示符

> 对应验收项：第 3 条（答案顺序正确、相邻两轮之间只有一个换行）。

**做什么**：答案按增量实时写到 stdout；每轮结束后只补**一个**换行；只有 stdin 是终端时才打印 `> `。

**提示**：

- 测试断言的是精确字符串 `"first\nsecond\n"`，多一个空行就挂
- 已有 `OnEvent` 负责写增量，你只需保证"写"和"补换行"不要重复
- 判断终端：`file, ok := r.(*os.File)` → `file.Stat()` → `info.Mode()&os.ModeCharDevice != 0`。不是 `*os.File`（比如测试里的 `strings.Reader`）就当不是终端

**完成标志**：管道输入时 stdout 里**没有** `> `，也没有多余空行。

## 4. 验收项 → 任务 → 测试

票上第 7 条是“既有测试全绿”，由全部测试覆盖，不单独列。

| 票的验收项 | 由哪个任务实现 | 验收测试 |
| --- | --- | --- |
| 1. 无参数 + 输入结束 → 退出码 0、无输出 | T2、T3 | `TestNoInputAndNoPromptExitsCleanly`、`TestReplEOFExitsCleanly` |
| 2. 三轮对话，第三次请求 5 条消息、角色交替 | T3 | `TestReplSendsGrowingHistory` |
| 3. 答案顺序正确，相邻两轮只一个换行 | T5 | `TestReplPrintsEveryAnswer` |
| 4. 空行/纯空白行不发请求 | T3 | `TestReplIgnoresBlankLines` |
| 5. 失败后循环继续，历史保留失败消息 | T4 | `TestReplRecordsFailedTurnInHistory` |
| 6. 两条路径共用同一 `Agent.Run` | T2 | `TestReplSendsGrowingHistory` + 既有单次提问测试 |

## 5. 执行顺序

1. T1 → `go build ./...` 过
2. T2 + T3 → `go test ./e2e -run 'Repl|NoInput' -count=1`，先让历史那几条变绿
3. T4 → `go test ./e2e -run 'FailedTurn' -count=1`
4. T5 → `go test ./... -count=1` 全绿
5. 手动跑一次管道 + 看录制文件

一次只推进一个任务，别五个一起改——红了会分不清是哪一步的锅。

## 6. 约束

- `ai` 包只加 `ErrorMessage` 一个字段，别的不动
- `agent.Agent.Run` 签名不变：`Run(ctx, history) ([]ai.Message, error)`
- 不引第三方依赖
- 注释用中文、说人话；类型声明逐行展开；函数体不写单行

## 7. 测试从哪来 / 卡住怎么办

测试在分支 `step-02-solution` 上，取到工作区但不提交：

```bash
git checkout step-02-solution -- e2e/multi_turn_test.go e2e/cli_test.go
```

卡住超过两小时再看参考实现（那个分支上也有实现，先别看）：

```bash
git diff 67ac58a..step-02-solution -- cmd/mypi agent
```

按 `AGENTS.md` 记 `step-02-ref`，并补一份「我卡在哪」的笔记放进 `docs/notes/`。

