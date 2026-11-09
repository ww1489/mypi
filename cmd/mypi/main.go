// Command mypi 是一个按教学节奏逐步搭建起来的最小编码 agent。
// 它的分层与 PiG 仓库保持一致，方便两边并排阅读。
package main

import (
	"os"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}
