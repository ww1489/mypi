// Command mypi is a minimal coding agent built step by step as a teaching
// project. It mirrors the layering of the PiG repository so the two can be read
// side by side.
package main

import (
	"os"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}
