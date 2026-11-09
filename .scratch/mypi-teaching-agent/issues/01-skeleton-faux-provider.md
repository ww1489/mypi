# 01: 骨架与假模型：`mypi "hi"` 打印脚本化回答

**What to build:** 仓库完成初始化（Go 模块、目录骨架、`AGENTS.md` 教学规范），并且 `mypi "hi"` 能在**完全离线**的情况下输出一份由脚本化假 provider 提供的确定性回答。`--help` 与 `--version` 不读取任何用户配置即可工作；非交互模式下 stdout 只包含回答内容，诊断信息走 stderr。假 provider 会把每次收到的请求录到文件，供测试断言。

**Blocked by:** None（可立即开始）

**Status:** done

- [x] `mypi "hi"` 在无网络、无密钥环境下输出与脚本文件一致的确定性文本
- [x] `mypi --help` 与 `mypi --version` 在不存在的配置目录下仍正常退出（退出码 0）
- [x] 非交互模式 stdout 只含回答、stderr 只含诊断，两者可分离重定向
- [x] 假 provider 的录制文件记录了本次请求的完整载荷，且格式稳定可解析
- [x] `go vet ./...` 与 `go test ./...` 全绿，测试从进程边界驱动、不依赖网络
- [x] `AGENTS.md` 记录了模块与目录映射、验收清单、密钥读取方式（只记方式不记值）

## 完成记录

- 实现：`b213be1`（骨架 + 假 provider + 进程级测试）
- 注释中文化：`c2a8b0a`、`11da98e`（并写进 `AGENTS.md` 的写作约定）
- 重构：`847cbcb`（`Message` 加 `Blocks()`，去掉 `Wire()` 里的重复 switch）
- 删死字段：`6fad78a`（`Request.System`）
- 开箱即用：`9c1d7fd`（内置脚本走 `go:embed`，`go run ./cmd/mypi "hi"` 零配置）
- 差异清单：`docs/compare/step-01.md`

## 验收命令

```bash
go vet ./... && go test ./...
go run ./cmd/mypi "hi"
go run ./cmd/mypi --version
```
