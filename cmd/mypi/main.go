// Command mypi 是一个边学边写的编码 agent。目录结构照着 PiG 来，方便对照。
package main

import (
	"os"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}
