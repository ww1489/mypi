# AGENTS.md

mypi 是一个教学项目：从零重写 PiG（`/data/code/PiG`）的最小内核，每一步都是一个可运行、可测试的中间态。

## 项目事实

- Go 模块 `mypi`，二进制 `mypi`，入口 `cmd/mypi`。
- 到票 10 为止只用标准库；票 11（TUI）允许 `golang.org/x/term`。
- 唯一测试接缝：`mypi` 进程边界 + 假 provider 的请求录制文件。不要在测试里 import 实现包去断言内部结构。
- 验证门槛：`go vet ./... && go test ./...`，两者都必须离线、无密钥、无网络。

## 与 PiG 的对应关系

| mypi | PiG | 说明 |
| --- | --- | --- |
| `ai/` | `ai/` | provider 契约、消息、流式事件 |
| `agent/` | `agent/` | 循环、工具调度、事件 |
| `tools/` | `internal/codingagent/tools/` | read / bash / write / edit |
| `session/` | `internal/codingagent/session*.go` | JSONL 会话树 |
| `cmd/mypi/` | `coding/cli/` + `cmd/pig/` | 命令行前端 |
| `e2e/` | `PiG/test/` | 进程级黑盒测试 |
| `docs/plan.md` | — | 8 步教学计划 |
| `docs/compare/step-NN.md` | — | 与 PiG 的差异清单 |
| `.scratch/…/issues/` | — | 切片票（本地 issue 列表） |

命名优先贴 PiG（`toolCallId`、`stopReason`、`parentId`），Go 惯用法优先于逐字翻译；两者冲突时选 Go 惯用，并在 `docs/compare/` 里记下理由。

## 写作约定

- **代码注释一律用中文**：包注释、函数注释、行内注释、测试注释都包括。
- 标识符、JSON 字段名、协议常量、命令输出与错误文案保持英文，不翻译。
- 文档（`docs/`、票、提交信息）用中文。
- **类型声明逐行展开**，不写 `type X struct{ A B }` 这类压缩写法。
- **函数体不用单行**，`func (x) Foo() { return 1 }` 要展开成三行。

## 密钥

- 只从环境变量（优先）或用户已有的 pig 配置（`~/.pig/agent/auth.json`）读取。
- 永不写入仓库、日志、会话文件或错误信息。
- 本文件只记录读取方式，不记录任何密钥值。

## 每步完成标准

1. `go vet ./...` 与 `go test ./...` 全绿。
2. 该票的验收命令手工跑一遍，输出符合预期。
3. 提交信息为 `step-NN: <一句话>`。
4. 若某步卡住超过两小时，允许直接取参考实现，记为 `step-NN-ref`，并补一份"我卡在哪"的笔记。

## 不要做

- 不要为"以后可能用到"提前抽象；每层只写当前票要求的行为。
- 不要引入第三方依赖替代标准库。
- 不要让测试访问网络、读取真实密钥或依赖当前时间。

## 票

`docs/plan.md` 是教学路线，`.scratch/mypi-teaching-agent/issues/` 是执行单元；票的编号顺序即依赖顺序。
