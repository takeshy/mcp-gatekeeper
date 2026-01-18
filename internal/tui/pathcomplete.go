package tui

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// PathCompleter provides path completion functionality
type PathCompleter struct{}

// NewPathCompleter creates a new path completer
func NewPathCompleter() *PathCompleter {
	return &PathCompleter{}
}

// Complete returns a list of path completions for the given prefix
func (pc *PathCompleter) Complete(prefix string) []string {
	if prefix == "" {
		prefix = "/"
	}

	// Expand ~ to home directory
	if strings.HasPrefix(prefix, "~") {
		home, err := os.UserHomeDir()
		if err == nil {
			prefix = strings.Replace(prefix, "~", home, 1)
		}
	}

	// Determine directory and file prefix
	dir := prefix
	filePrefix := ""

	if !strings.HasSuffix(prefix, "/") {
		dir = filepath.Dir(prefix)
		filePrefix = filepath.Base(prefix)
	}

	// Read directory
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	var completions []string
	for _, entry := range entries {
		name := entry.Name()

		// Skip hidden files unless prefix starts with .
		if strings.HasPrefix(name, ".") && !strings.HasPrefix(filePrefix, ".") {
			continue
		}

		// Filter by prefix
		if filePrefix != "" && !strings.HasPrefix(name, filePrefix) {
			continue
		}

		fullPath := filepath.Join(dir, name)
		if entry.IsDir() {
			fullPath += "/"
		}

		completions = append(completions, fullPath)
	}

	sort.Strings(completions)

	// Limit to 10 completions
	if len(completions) > 10 {
		completions = completions[:10]
	}

	return completions
}

// GetCurrentPathFromLine extracts the path being typed from a line
func (pc *PathCompleter) GetCurrentPathFromLine(line string) string {
	// Find the last path-like string in the line
	line = strings.TrimSpace(line)

	// If empty, return empty
	if line == "" {
		return ""
	}

	// For glob patterns, we need the base path
	// e.g., "/home/user/**" -> we want to complete "/home/user/"
	// e.g., "/home/us" -> we want to complete "/home/us"

	// Remove glob characters from the end
	path := strings.TrimRight(line, "*?")

	return path
}

// ReplacePathInLine replaces the current path in the line with the completion
func (pc *PathCompleter) ReplacePathInLine(line, completion string) string {
	currentPath := pc.GetCurrentPathFromLine(line)
	if currentPath == "" {
		return completion
	}

	// Keep any glob suffix
	suffix := ""
	trimmed := strings.TrimRight(line, "*?")
	if len(trimmed) < len(line) {
		suffix = line[len(trimmed):]
	}

	return completion + suffix
}
