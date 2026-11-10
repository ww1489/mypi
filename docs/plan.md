# Plan: mypi — 从零写一个 PiG 式 Agent（8 步教学）

## TL;DR

在空目录 `/data/code/mypi` 里从零长出一个 Go agent（module `mypi`，二进制 `mypi`），分层与 `/data/code/PiG` 同名同构（`ai/` `agent/` `tools/` `session/` `cmd/mypi/`）。8 个 Step，每步 = 一个可 `go run` 的 commit + 一份我给的验收测试。分工：**我出任务卡/验收测试/对照笔记，你写核心逻辑**（循环、工具调度、会话树、compaction），纯苦力代码（SSE 分帧解析、参数校验）我直接给实现。参照策略为混合：先闭卷写，完成后我对照 PiG 精确文件生成差异清单。

## 前提共识（两轮质问已锁定）

- Go 1.27.0（已装于 `~/.local/go`），零第三方依赖到 Step 6，TUI 加餐才允许 `x/term`
- 终点：B 线（可跑 agent + 事件系统 + JSONL 会话树 + compaction + 4 工具）+ C 加餐（最小 TUI）；扩展/MCP/RPC 只讲不写
- 概念等价而非 byte-parity：复刻关键设计（事件化、工具声明增量、会话树、并行工具源顺序），不追 42 家 provider/精确 prompt 文本
- 真模型只实现 `openai-completions` 一种协议，provider 用 deepseek（`~/.pig/agent/auth.json` 已配）
- 一步一 commit（`step-N: …`），`git init`，`mypi/AGENTS.md` 作为项目内规范与上下文锚点

## Steps

### Phase 1 · 骨架与循环（Step 0–1）

1. **Step 0 · 骨架与假模型** — `go.mod`（module mypi）+ `cmd/mypi/main.go` + `ai/` 下脚本化 `faux` provider + 最朴素的 `chan Event` 流；`mypi "hi"` 输出假回复。验收：`go test ./...` 绿 + 命令可复现。
   参照：`PiG/ai/faux.go`、`PiG/ai/event_stream.go`、`PiG/ai/provider_streams.go`（只看形状，不上重量级实现）
2. **Step 1 · 循环** — *depends on 0*。单轮流式请求、无工具、多轮 REPL；消息建模用 `interface Message` + type switch，JSON tag 镜像 pig（`role`/`stopReason`…）。验收：faux 脚本驱动下 3 轮对话状态正确。
   参照：`PiG/agent/standalone_loop.go`（`RunAgentLoop`，~145 行骨架）、`PiG/agent/agent_loop.go`（`loop` / `runTurn` / `streamAssistantResponse`）、`PiG/agent/agent.go`（消息类型）

### Phase 2 · 真模型（Step 2）

3. **Step 2 · 真 provider** — *depends on 1*。`openai-completions` SSE 解析 + deepseek 接入（key 从 `DEEPSEEK_API_KEY` 或 `~/.pig/agent/auth.json` 读）；faux 降级为测试专用。验收：真实多轮纯文本问答跑通；流式 token 增量打印。含工具调用的问答等 Step 3 有工具后再验。
   参照：`PiG/ai/sse.go`、`PiG/ai/openai_responses.go` 之外的同族 `openai*.go`、`PiG/ai/api_streams.go`（`StreamOpenAICompletions`）、`PiG/ai/types.go`（`StreamOptions`/`Provider` 形状）
   苦力代码（SSE 分帧、tool_call delta 拼接）由我直接给

### Phase 3 · 工具与接口演进（Step 3–4）

4. **Step 3 · 工具（极简接口）** — *depends on 2*。`Execute(ctx, json.RawMessage) (string, error)` + `read` + `bash`；跑通"工具调用 → 结果回灌 → 再次请求"闭环；工具执行走串行。验收：agent 能读文件并回答内容；bash 输出被正确回灌；真实模型含工具调用的问答跑通。
   参照：`PiG/agent/run_tool_call.go`、`PiG/internal/codingagent/tools/read.go`、`bash.go`（截断策略 2000 行/50KB 必复刻）
5. **Step 4 · 接口长成 pig 形状** — *depends on 3*。工具接口扩为 `Name/Label/Schema/Execute(ctx, toolCallID, params, onUpdate)/ExecutionMode`；并行执行 + 按源顺序落 tool-result；**工具声明以 system message 增量下发**（`toolsAdded`/`toolsRemoved`）。验收：并行批的结果顺序严格等于调用顺序；增删工具后 system prompt 的 diff 正确。
   参照：`PiG/agent/agent.go`（`AgentTool`）、`PiG/agent/system_state.go`（`declareToolChanges`）、`PiG/agent/tool_execution.go`、`PiG/ai/GetToolStateChangesFrom`
   可选能力（`PrepareArguments`/`OutputSchema`/`QueueOrderable` 的类型断言机制）只讲解不实现

### Phase 4 · 事件化与会话树（Step 5）

6. **Step 5 · 事件化 + 会话树** — *depends on 4*。`chan Event` 升级为 `EventStream`（有序/无界缓冲/可 `Result()`/懒启动），并记录"为什么 chan 不够"；会话写 JSONL + `parentId` 树，支持 `--resume`/`--fork`。验收：会话文件能被 `jq` 逐行读懂；fork 后两条分支独立对话；事件顺序快照测试。
   参照：`PiG/ai/event_stream.go`、`PiG/internal/codingagent/session_file.go`、`session_manager.go`、`PiG/internal/sessionentry/`、`docs/site/docs/session-format.md`

### Phase 5 · 上下文管理（Step 6）

7. **Step 6 · compaction** — *depends on 5*。token 估算 + 切点规则（**绝不切开 tool call 与其 result**）+ `/compact` 与阈值自动触发（reserve ≈16k / keepRecent ≈20k）。验收：构造超长会话触发压缩，压缩后仍能继续对话且工具调用序列合法。
   参照：`PiG/internal/codingagent/compaction/compaction.go`（`FindCutPoint`/`PrepareCompaction`）、`PiG/agent/context_tokens.go`、`docs/site/docs/compaction.md`

### Phase 6 · 可选加餐（Step 7）

8. **Step 7（加餐）· 最小 TUI** — *depends on 6*。`Component{Render(width) []string; Invalidate()}` 差分渲染 + 单线程事件循环（agent 事件与按键在同一 goroutine 上 dispatch，慢活丢后台 goroutine 再 marshal 回来）。验收：能滚动、能输入、能看到工具执行块。
   参照：`PiG/tui/tui.go`、`PiG/internal/codingagent/interactive_events.go`（设计意图注释是核心教材）

## 每步的固定任务卡结构

我每步给：① 本步要回答的设计问题（一句话）；② 接口签名与目录约束；③ 验收测试（`go test` 可跑的 `_test.go`）；④ 验收命令。你完成后回一句"这步过了"，我产 `mypi/docs/compare/step-NN.md`（PiG 精确文件:行号 + 差异清单 + 遗漏的设计意图），再进下一步。

## Relevant files

**要创建（`/data/code/mypi`）**

- `go.mod` — module `mypi`，go 1.27
- `AGENTS.md` — 项目内规范：与 PiG 的对应表、每步验收清单
- `cmd/mypi/main.go` — 入口（参数解析手写，不引 cobra，镜像 PiG 的 `coding/cli/args.go` 风格）
- `ai/` — `faux.go`、`event_stream.go`、`openai_completions.go`、`sse.go`、`types.go`
- `agent/` — `agent.go`（`Message` 接口族 + `Tool` 接口）、`loop.go`（`Run`/`runTurn`）、`tools_exec.go`、`system_state.go`（工具声明增量）
- `tools/` — `read.go`、`bash.go`、`edit.go`、`write.go`
- `session/` — `entry.go`、`file.go`（JSONL 解析）、`manager.go`（目录编码/`--resume`）
- `internal/` — `tokens.go`（估算）、`compaction.go`
- `docs/compare/step-NN.md` — 我生成的对照笔记
- `tui/` — 第 7 步才有

**参照（`/data/code/PiG`，只读）**

- `docs/site/docs/how-pig-works.md` — 最好的架构导览，Step 1 前必读
- `docs/pig-architecture.md` — 分层图与 Pi↔PiG 包映射
- `docs/site/docs/session-format.md`、`compaction.md`、`sdk.md`
- `agent/standalone_loop.go`、`agent/agent_loop.go`、`agent/agent.go`、`agent/system_state.go`、`agent/tool_execution.go`
- `ai/faux.go`、`ai/event_stream.go`、`ai/api_streams.go`、`ai/sse.go`、`ai/types.go`
- `internal/codingagent/tools/{read,bash,edit,write}.go`、`internal/codingagent/session_file.go`、`internal/codingagent/compaction/compaction.go`
- `internal/codingagent/prompts/coding.go`（system prompt 分节顺序：tools→rules→docs→addendum→project_context→skills→cwd）
- `tui/tui.go`、`internal/codingagent/interactive_events.go`

## Verification

1. 每步门槛：`cd /data/code/mypi && go vet ./... && go test ./...` 全绿 + 该步的"验收命令"手工跑一遍有预期输出
2. Step 0–1、Step 3–4 全部离线可验（faux provider 脚本化响应，无网络）
3. Step 2 需真网络：`DEEPSEEK_API_KEY=… go run ./cmd/mypi "你好"`，验证流式增量；Step 3 再用 `DEEPSEEK_API_KEY=… go run ./cmd/mypi "读一下 go.mod 第一行"` 验证工具回灌
4. Step 5 用 `jq -c . ~/.mypi/sessions/<dir>/*.jsonl` 逐行核对 `parentId` 链；fork 后两个 leaf 各自可 `--resume`
5. Step 6 用测试构造 > 阈值的长会话，断言压缩后 `FindCutPoint` 不落在 tool call/result 之间
6. 全程回归：`go test ./... -count=1`，并每步 git commit（`step-N: …`）

## Decisions

- 语言/形态：Go 1.27，module `mypi`，目录名与 PiG 同名以便零翻译对照
- 分工：你写核心逻辑，我出题/验收/评审；SSE 分帧与参数校验等苦力代码我给
- 参照方式：闭卷先写 → 完成后对照 PiG 生成 `docs/compare/step-NN.md` 差异清单
- 消息建模：`interface Message` + type switch（Go 惯用），JSON tag 镜像 pig
- 流式：Step 0 用最朴素 `chan Event`，Step 5 升级 `EventStream`（保留"为什么"的教学）
- 工具接口：Step 3 极简签名，Step 4 扩为 pig 形状；可选能力仅讲解
- 依赖：Step 0–6 零第三方依赖；Step 7 允许 `x/term`
- 范围外：42 家 provider、精确 prompt 文本、扩展宿主/MCP/RPC/Piglet、oracle parity 测试、Windows

## Further Considerations

1. **卡住时的逃生舱**：若某步超过 2 小时不通过，是否允许我直接给该步完整参考实现，你再回读？推荐允许（记为 `step-N-ref`），避免学习节奏被单点阻塞。
2. **deepseek key 落点**：优先 `DEEPSEEK_API_KEY`，回退读 `~/.pig/agent/auth.json`；是否要同时写进 `mypi/AGENTS.md` 说明（避免把密钥写进仓库）？
3. **Step 3 的 bash 安全边界**：教学版是否加最小限制（超时 + 输出截断 + 不接管 stdin），还是完全裸奔由你负责？推荐加最小限制，与 PiG 的 `/50KB` 截断对齐。
