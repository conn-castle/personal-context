package main

import (
	"fmt"
	"io"
	"os"

	"github.com/conn-castle/personal-context/cli/internal/cli"
)

var version = cli.DefaultVersion
var exitFn = os.Exit

// run executes the root CLI command with explicit args and I/O streams.
// It returns 0 on success and 1 when command execution fails.
func run(args []string, stdout io.Writer, stderr io.Writer) int {
	root := cli.NewRootCommand(cli.RootCommandOptions{
		Stdout:  stdout,
		Stderr:  stderr,
		Version: version,
	})
	root.SetArgs(args)

	if err := root.Execute(); err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return 1
	}

	return 0
}

func main() {
	exitFn(run(os.Args[1:], os.Stdout, os.Stderr))
}
