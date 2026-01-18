package executor

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// NormalizedCommand represents a normalized command
type NormalizedCommand struct {
	Cwd     string   // Normalized working directory (realpath)
	Cmd     string   // Normalized command (resolved via PATH if needed)
	Args    []string // Command arguments
	Cmdline string   // Full command line for policy matching
}

// Normalizer normalizes commands for consistent policy evaluation
type Normalizer struct{}

// NewNormalizer creates a new Normalizer
func NewNormalizer() *Normalizer {
	return &Normalizer{}
}

// Normalize normalizes a command request
func (n *Normalizer) Normalize(cwd, cmd string, args []string) (*NormalizedCommand, error) {
	result := &NormalizedCommand{
		Args: args,
	}

	// Normalize CWD using realpath
	normalizedCwd, err := n.normalizePath(cwd)
	if err != nil {
		// If cwd doesn't exist, use as-is but note it
		result.Cwd = cwd
	} else {
		result.Cwd = normalizedCwd
	}

	// Normalize command
	result.Cmd = n.normalizeCommand(cmd)

	// Build full command line for policy matching
	result.Cmdline = n.buildCmdline(result.Cmd, args)

	return result, nil
}

// normalizePath resolves a path to its real path (following symlinks)
func (n *Normalizer) normalizePath(path string) (string, error) {
	// First make it absolute
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}

	// Then resolve symlinks
	realPath, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		// If path doesn't exist, return absolute path
		if os.IsNotExist(err) {
			return absPath, nil
		}
		return "", err
	}

	return realPath, nil
}

// normalizeCommand resolves a command to its full path if possible
func (n *Normalizer) normalizeCommand(cmd string) string {
	// If it's already an absolute path, resolve symlinks
	if filepath.IsAbs(cmd) {
		if realPath, err := filepath.EvalSymlinks(cmd); err == nil {
			return realPath
		}
		return cmd
	}

	// If it contains a slash, it's a relative path
	if strings.Contains(cmd, "/") {
		if absPath, err := filepath.Abs(cmd); err == nil {
			if realPath, err := filepath.EvalSymlinks(absPath); err == nil {
				return realPath
			}
			return absPath
		}
		return cmd
	}

	// Look up in PATH
	if path, err := exec.LookPath(cmd); err == nil {
		if realPath, err := filepath.EvalSymlinks(path); err == nil {
			return realPath
		}
		return path
	}

	// Return as-is if not found
	return cmd
}

// buildCmdline builds the full command line string
func (n *Normalizer) buildCmdline(cmd string, args []string) string {
	parts := make([]string, 0, len(args)+1)
	parts = append(parts, n.quoteIfNeeded(cmd))
	for _, arg := range args {
		parts = append(parts, n.quoteIfNeeded(arg))
	}
	return strings.Join(parts, " ")
}

// quoteIfNeeded adds quotes around a string if it contains spaces or special chars
func (n *Normalizer) quoteIfNeeded(s string) string {
	if strings.ContainsAny(s, " \t\n\"'\\$`") {
		// Escape double quotes and backslashes
		escaped := strings.ReplaceAll(s, "\\", "\\\\")
		escaped = strings.ReplaceAll(escaped, "\"", "\\\"")
		return "\"" + escaped + "\""
	}
	return s
}

// ParseCmdline parses a command line string into command and arguments
func ParseCmdline(cmdline string) (cmd string, args []string, err error) {
	parts, err := splitCmdline(cmdline)
	if err != nil {
		return "", nil, err
	}
	if len(parts) == 0 {
		return "", nil, nil
	}
	return parts[0], parts[1:], nil
}

// splitCmdline splits a command line string respecting quotes
func splitCmdline(cmdline string) ([]string, error) {
	var parts []string
	var current strings.Builder
	inQuote := false
	quoteChar := rune(0)
	escape := false

	for _, r := range cmdline {
		if escape {
			current.WriteRune(r)
			escape = false
			continue
		}

		if r == '\\' && !inQuote {
			escape = true
			continue
		}

		if r == '"' || r == '\'' {
			if !inQuote {
				inQuote = true
				quoteChar = r
				continue
			}
			if r == quoteChar {
				inQuote = false
				quoteChar = 0
				continue
			}
		}

		if (r == ' ' || r == '\t') && !inQuote {
			if current.Len() > 0 {
				parts = append(parts, current.String())
				current.Reset()
			}
			continue
		}

		current.WriteRune(r)
	}

	if current.Len() > 0 {
		parts = append(parts, current.String())
	}

	return parts, nil
}
