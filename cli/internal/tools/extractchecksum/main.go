//go:build tools
// +build tools

package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode"
)

const sha256Prefix = "sha256:"

func main() {
	os.Exit(run(os.Args, os.Stdout, os.Stderr))
}

// run executes the checksum extraction CLI.
// Args: args contains argv0, checksum file path, and target filename; out and errOut receive command output.
// Returns: zero on successful extraction, non-zero when input validation or lookup fails.
func run(args []string, out io.Writer, errOut io.Writer) int {
	if len(args) != 3 {
		fmt.Fprintf(errOut, "Usage: %s <checksums-file> <target-filename>\n", args[0])
		return 1
	}

	checksumsPath := args[1]
	target := args[2]

	file, err := os.Open(checksumsPath)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Fprintf(errOut, "Error: %s not found\n", checksumsPath)
			return 1
		}
		fmt.Fprintf(errOut, "Error: failed to read %s: %v\n", checksumsPath, err)
		return 1
	}
	defer file.Close()

	targetTrimmed := strings.TrimLeft(target, "./")
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		idx := strings.IndexFunc(line, unicode.IsSpace)
		if idx <= 0 {
			continue
		}
		checksum := line[:idx]
		filename := strings.TrimSpace(line[idx:])
		if strings.HasPrefix(filename, "*") {
			filename = strings.TrimSpace(strings.TrimPrefix(filename, "*"))
		}
		filename = strings.TrimLeft(filename, "./")
		if filename == target || filename == targetTrimmed {
			checksum = strings.TrimPrefix(checksum, sha256Prefix)
			fmt.Fprintln(out, checksum)
			return 0
		}
	}

	if err := scanner.Err(); err != nil {
		fmt.Fprintf(errOut, "Error: failed to read %s: %v\n", checksumsPath, err)
		return 1
	}

	fmt.Fprintf(errOut, "Error: %s not found in %s\n", target, checksumsPath)
	return 1
}
