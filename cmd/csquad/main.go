// Command csquad manages teams of Claude Code and Codex sessions in tmux.
package main

import (
	"os"

	"github.com/ShunL12324/c-squad/internal/cli"
)

func main() {
	if err := cli.Run(os.Args[1:]); err != nil {
		os.Exit(cli.Report(os.Stderr, err))
	}
}
