//go:build tools
// +build tools

package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
)

var (
	urlPattern = regexp.MustCompile(`(?m)^(\s*url\s+").*("\s*)$`)
	shaPattern = regexp.MustCompile(`(?m)^(\s*sha256\s+").*("\s*)$`)
)

func main() {
	os.Exit(run(os.Args, os.Stderr))
}

// run executes the Homebrew formula updater CLI.
// Args: args contains argv0, formula path, new URL, and new SHA-256; errOut receives diagnostics.
// Returns: zero after replacing the single url and sha256 lines, non-zero on validation or write failure.
func run(args []string, errOut io.Writer) int {
	if len(args) != 4 {
		fmt.Fprintf(errOut, "Usage: %s <formula-file> <new-url> <new-sha256>\n", args[0])
		return 1
	}

	formulaPath := args[1]
	newURL := args[2]
	newSHA := args[3]

	info, err := os.Stat(formulaPath)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Fprintf(errOut, "Error: %s not found\n", formulaPath)
			return 1
		}
		fmt.Fprintf(errOut, "Error: failed to stat %s: %v\n", formulaPath, err)
		return 1
	}

	content, err := os.ReadFile(formulaPath)
	if err != nil {
		fmt.Fprintf(errOut, "Error: failed to read %s: %v\n", formulaPath, err)
		return 1
	}

	text := string(content)
	urlMatches := urlPattern.FindAllStringSubmatch(text, -1)
	if len(urlMatches) != 1 {
		fmt.Fprintf(errOut, "Error: expected 1 url line, found %d\n", len(urlMatches))
		return 1
	}
	shaMatches := shaPattern.FindAllStringSubmatch(text, -1)
	if len(shaMatches) != 1 {
		fmt.Fprintf(errOut, "Error: expected 1 sha256 line, found %d\n", len(shaMatches))
		return 1
	}

	text = replaceLine(urlPattern, text, newURL)
	text = replaceLine(shaPattern, text, newSHA)

	if err := writeFileAtomic(formulaPath, []byte(text), info.Mode()); err != nil {
		fmt.Fprintf(errOut, "Error: failed to write %s: %v\n", formulaPath, err)
		return 1
	}

	return 0
}

// replaceLine replaces the value inside a matched quoted line while preserving indentation.
// Args: pattern captures the prefix and suffix; text is the full file content; value is the replacement.
// Returns: text with every matching line updated.
func replaceLine(pattern *regexp.Regexp, text string, value string) string {
	return pattern.ReplaceAllStringFunc(text, func(match string) string {
		parts := pattern.FindStringSubmatch(match)
		if len(parts) < 3 {
			return match
		}
		return parts[1] + value + parts[2]
	})
}

// writeFileAtomic writes path via a temporary sibling file and rename.
// Args: path is the target file, data is the full replacement content, and mode is the target file mode.
// Returns: an error if the temporary file cannot be written or renamed into place.
func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	base := filepath.Base(path)
	tmp, err := os.CreateTemp(dir, "."+base+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	cleanup = false
	return nil
}
