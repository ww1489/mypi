# 票 02 任务卡：多轮对话循环

## 1. 这次要做成什么样

现在 mypi 只会回答一句就退出。这次要让它能一直聊下去。

做完之后应该有这些行为：

- 命令行里写了问题（`mypi "你好"`）：和现在一样，问一次、答一次、退出
- 命令行里没写问题（就敲一个 `mypi`）：不再报错退出，而是开始等你输入
- 你打一句回车，它答一句；再打一句，它再答一句
- 它要“记得”前面说过的话。不是它真的记得，是我们每次都把之前全部对话重新发过去
- 你按 Ctrl-D（或者输入结束，比如管道读完了）：正常退出，退出码 0，不打印一堆错误

怎么判断做完了：

```bash
printf '第一问\n第二问\n' | go run ./cmd/mypi
```

应该看到两段回答。想知道它到底发了什么，再加个录制文件：

```bash
printf '第一问\n第二问\n' | MYPY_FAUX_RECORD=/tmp/r.jsonl go run ./cmd/mypi
cat /tmp/r.jsonl
```

第二次请求里应该有三条消息：你问的第一句、它答的第一句、你问的第二句。

为什么非要重发：模型每次只看得见这一次发给它的东西，它没有记忆。所谓“记得”，就是我们替它把历史攒着、每轮重发一遍。

## 2. 现在的代码长什么样

先看清现状。下面每一条，都是后面某个步骤要解决的问题。

| 现在的情况 | 在哪 | 所以现在做不成什么 |
| --- | --- | --- |
| `run` 的参数只有「命令行参数、标准输出、错误输出」，拿不到键盘输入 | `cmd/mypi/cli.go` 里的 `func run(...)` | 没法读你打进来的字 |
| 只要命令行里没写问题，就打印用法然后退出（退出码 2） | `cmd/mypi/cli.go` 里 `if strings.TrimSpace(opts.prompt) == ""` 那段 | 单独敲 `mypi` 根本进不去对话 |
| 模型调用失败时，`Run` 把错误丢给调用方，历史里什么都不留 | `agent/agent.go` 里 `case ai.ErrorEvent: return history, e.Err` | 出错的那一轮看起来像你什么都没问 |

## 3. 任务拆解

### 第一步：让 `run` 能读到键盘输入

> 这一步不直接对应哪条验收，但后面每步都靠它。

**要做什么**：给 `run` 加一个参数专门用来读输入，`main` 里把 `os.Stdin` 传进去。

**具体怎么改**：

- 现在是 `run(args []string, stdout, stderr io.Writer) int`
- 改成 `run(args []string, stdin io.Reader, stdout, stderr io.Writer) int`
- `cmd/mypi/main.go` 里调用它的地方，跟着加一个 `os.Stdin`

**怎么算做完**：`go build ./...` 能过，`go test ./...` 里原来那些测试还是绿的。

---

### 第二步：没写问题的时候别退出，改成开始对话

> 对应票里第 1 条（无参数时干净退出）和第 6 条（两条路走同一条路径）。

**要做什么**：把「没写问题就打印用法、返回 2」这段整个删掉，换成分岔：

- 写了问题 → 还是老样子，问一次、打印答案、返回 0
- 没写问题 → 进入第三步写的循环

**具体怎么改**：

- 删的是**整个 if 段**（连里面的 `fmt.Fprint(stderr, ...)` 和 `return exitUsage` 一起），不是把 `exitUsage` 换个数字
- 分岔点放在 `ag := &agent.Agent{...}` 之后。provider、`OnEvent`、`ctx` 都只建一次，两条路共用
- `OnEvent` 里会往 `out` 写东西，两条路都要能用到它

**怎么算做完**：跑 `go run ./cmd/mypi`，不再打印 missing prompt，而是停在那里等输入；按 Ctrl-D 能退回命令行，退出码 0。

---

### 第三步：写那个「读一句、问一次」的循环

> 对应票里第 1 条（读到结尾正常退出）、第 2 条（历史越攒越长）、第 4 条（空行不问）。

**要做什么**：一个循环，每次做这几件事：

1. 从输入里读一行；读不到了（输入结束）就跳出循环
2. 去掉首尾空白；如果剩下是空的，直接看下一行，不要问模型
3. 把「之前攒下的全部消息 + 刚读到的这一句」交给 `agent.Run`
4. 把 `Run` 返回的历史存下来，留给下一轮用
5. 打印这一轮的答案，然后回到第 1 步

循环结束后返回 0。

**几个容易踩的坑**：

- `bufio.Scanner` 默认一行最多 64KB，粘一段长文本会直接报错。先用 `scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)` 把上限放宽
- 历史要放在**循环外面**声明。放里面就每轮从零开始，模型永远记不住上一句
- 拼「历史 + 这一句」的时候要复制一份再拼，不要把新元素直接加进调用方那个切片（第四步会解释为什么）
- 读到结尾是正常结束，返回 0，不要返回 1

**怎么算做完**：喂三行输入，录制文件里应该有三行请求，消息条数分别是 1、3、5，角色依次是 user、assistant 交替。

---

### 第四步：出错的时候，别把这一轮抹掉

> 对应票里第 5 条（报错后继续，历史里留下失败记录）。

**要做什么**：现在 `Run` 出错就 `return history, e.Err`，历史原样返回。改成：

1. 造一条 assistant 消息：内容为空、`StopReason` 是 error、把错误文本放进新加的 `ErrorMessage` 字段
2. 把它接在历史后面
3. 把这份新历史和错误**一起**返回

调用方那边（`cli.go`）顺序要反过来：**先把返回的历史存下来，再看 err**。出错就打印到 stderr，然后继续读下一句。

**具体怎么改**：

- `ai/types.go` 的 `AssistantMessage` 加一个字段：`ErrorMessage string`，tag 是 `json:"errorMessage,omitempty"`。这是这一步唯一允许动 `ai` 包的地方
- 拼历史时不要 `append(history, x)`。如果 `history` 底层还有空位，这会写进调用方的数组里，把别人的数据改掉。先复制：`out := append(make([]ai.Message, 0, len(history)+1), history...)`

**怎么算做完**：第一次请求失败、第二次成功时，第二次请求里应该有 3 条消息：user、assistant、user。

---

### 第五步：把输出格式抠准

> 对应票里第 3 条（答案顺序对、两轮之间只有一个换行）。

**要做什么**：

- 答案边生成边打印（现在就是这么写的，别改成攒完再打）
- 每答完一句，只输出**一个**换行
- 从管道读输入的时候不要打印 `> ` 提示符

**几个坑**：

- 测试要求 stdout 正好是 `"first\nsecond\n"`。多一个空行、少一个换行都会挂
- 现在 `OnEvent` 负责写增量；补换行的地方单独写，别两处都补
- 判断“是不是在终端里”：把输入当 `*os.File` 断言一下，再 `Stat()`，看 `info.Mode()&os.ModeCharDevice != 0`。测试里喂的是 `strings.Reader`，断言会失败，那就当作不是终端（正好，不打印提示符）

**怎么算做完**：`go test ./e2e -run 'PrintsEveryAnswer' -count=1` 通过。

## 4. 票里的每条验收，由哪一步做、用哪条测试验

| 票里的验收（大白话） | 谁负责 | 哪条测试 |
| --- | --- | --- |
| 1. 只敲 `mypi`、马上按 Ctrl-D：退出码 0，什么也不输出 | 第二步 + 第三步 | `TestNoInputAndNoPromptExitsCleanly`、`TestReplEOFExitsCleanly` |
| 2. 连问三轮，第三次请求里有 5 条消息，人和模型交替 | 第三步 | `TestReplSendsGrowingHistory` |
| 3. 两段答案按顺序出来，中间只有一个换行 | 第五步 | `TestReplPrintsEveryAnswer` |
| 4. 空行、只有空格的行不问模型 | 第三步 | `TestReplIgnoresBlankLines` |
| 5. 某一轮出错：错误打到 stderr，接着问下一句，历史里留着那条失败消息 | 第四步 | `TestReplRecordsFailedTurnInHistory` |
| 6. 写问题和不写问题，走的是同一个 `Agent.Run` | 第二步 | 上面那条 + 原来单次提问的测试 |
| 7. 除「缺 prompt 报错」改成「无输入干净退出」外，原有测试都要绿 | 全部 | `go test ./...` |

## 5. 按什么顺序做

一次只做一步，做完就跑一次测试。五步一起改的话，红了不知道是谁的锅。

```bash
# 第一步做完
go build ./...

# 第二、三步做完
go test ./e2e -run 'Repl|NoInput' -count=1

# 第四步做完
go test ./e2e -run 'FailedTurn' -count=1

# 第五步做完
go vet ./... && go test ./... -count=1

# 最后手动跑一遍
printf '第一问\n第二问\n' | go run ./cmd/mypi
```

## 6. 不能做的事

- `ai` 包这一票只加 `ErrorMessage` 一个字段，别的不许动
- `agent.Agent.Run` 的签名不许变，还是 `Run(ctx, history) ([]ai.Message, error)`
- 不引第三方依赖
- 注释用中文、说人话；类型声明不要压成一行；函数体不要写单行

## 7. 测试从哪来，卡住了怎么办

测试在分支 `step-02-solution` 上。取到工作区别提交，跟你的实现一起提交：

```bash
git checkout step-02-solution -- e2e/multi_turn_test.go e2e/cli_test.go
```

卡住超过两小时再看参考实现（同一分支上也有实现，先别看）：

```bash
git diff 67ac58a..step-02-solution -- cmd/mypi agent
```

看之前先按 `AGENTS.md` 记一笔 `step-02-ref`，再写一份「我卡在哪」放进 `docs/notes/`。

