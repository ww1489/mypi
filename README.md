# mypi

边学边写一个编码 agent。目录结构照着 [PiG](https://github.com/MichaelKinsy/PiG) 来，每一步都是一个能跑、能测的中间态。

## 直接跑

```bash
go run ./cmd/mypi "hello"
```

不需要任何环境变量，也不需要密钥：默认走内置的假模型脚本。

想看 `--help`、`--version`：

```bash
go run ./cmd/mypi -h
go run ./cmd/mypi --version
```

## 验证

```bash
go vet ./... && go test ./...
```

全部离线，不读密钥，不依赖网络。

## 换成自己的假回复

```bash
printf '%s' '{"responses":[{"chunks":["你好","，我是假模型"]}]}' > /tmp/s.json
MYPY_FAUX_SCRIPT=/tmp/s.json go run ./cmd/mypi "hi"
```

想知道 agent 到底发了什么，再加一个录制文件：

```bash
MYPY_FAUX_RECORD=/tmp/rec.jsonl MYPY_FAUX_SCRIPT=/tmp/s.json go run ./cmd/mypi "hi"
cat /tmp/rec.jsonl
```

## 目录

| 目录 | 内容 |
| --- | --- |
| `cmd/mypi` | 命令行入口，手写参数解析 |
| `ai` | provider 接口、消息类型、流式事件、假 provider |
| `agent` | 调用 provider 并把结果拼回历史 |
| `e2e` | 进程级黑盒测试 |
| `docs/plan.md` | 8 步教学计划 |
| `docs/compare/` | 每步与 PiG 的差异 |
| `.scratch/…/issues/` | 切片票 |
| `AGENTS.md` | 项目内规范（写码前先看） |
