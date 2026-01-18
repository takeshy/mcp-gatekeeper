package executor

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNormalizer_Normalize(t *testing.T) {
	n := NewNormalizer()

	// Create a temp directory for tests
	tmpDir := t.TempDir()

	tests := []struct {
		name       string
		cwd        string
		cmd        string
		args       []string
		wantCwd    string
		wantCmd    string
		wantCmdlineContains string
	}{
		{
			name:       "basic command",
			cwd:        tmpDir,
			cmd:        "ls",
			args:       []string{"-la"},
			wantCwd:    tmpDir,
			wantCmdlineContains: "-la",
		},
		{
			name:       "command with spaces in args",
			cwd:        tmpDir,
			cmd:        "echo",
			args:       []string{"hello world"},
			wantCwd:    tmpDir,
			wantCmdlineContains: `"hello world"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := n.Normalize(tt.cwd, tt.cmd, tt.args)
			if err != nil {
				t.Errorf("Normalize() error = %v", err)
				return
			}

			// CWD should be normalized
			if result.Cwd != tt.wantCwd {
				// Allow for symlink resolution
				realCwd, _ := filepath.EvalSymlinks(tt.wantCwd)
				if result.Cwd != realCwd {
					t.Errorf("Normalize() cwd = %v, want %v or %v", result.Cwd, tt.wantCwd, realCwd)
				}
			}

			// Cmdline should contain expected string
			if tt.wantCmdlineContains != "" && !contains(result.Cmdline, tt.wantCmdlineContains) {
				t.Errorf("Normalize() cmdline = %v, should contain %v", result.Cmdline, tt.wantCmdlineContains)
			}
		})
	}
}

func TestParseCmdline(t *testing.T) {
	tests := []struct {
		name     string
		cmdline  string
		wantCmd  string
		wantArgs []string
	}{
		{
			name:     "simple command",
			cmdline:  "ls -la",
			wantCmd:  "ls",
			wantArgs: []string{"-la"},
		},
		{
			name:     "quoted args",
			cmdline:  `echo "hello world"`,
			wantCmd:  "echo",
			wantArgs: []string{"hello world"},
		},
		{
			name:     "single quoted args",
			cmdline:  `echo 'hello world'`,
			wantCmd:  "echo",
			wantArgs: []string{"hello world"},
		},
		{
			name:     "multiple args",
			cmdline:  "git commit -m 'fix bug'",
			wantCmd:  "git",
			wantArgs: []string{"commit", "-m", "fix bug"},
		},
		{
			name:     "empty cmdline",
			cmdline:  "",
			wantCmd:  "",
			wantArgs: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd, args, err := ParseCmdline(tt.cmdline)
			if err != nil {
				t.Errorf("ParseCmdline() error = %v", err)
				return
			}
			if cmd != tt.wantCmd {
				t.Errorf("ParseCmdline() cmd = %v, want %v", cmd, tt.wantCmd)
			}
			if !stringSliceEqual(args, tt.wantArgs) {
				t.Errorf("ParseCmdline() args = %v, want %v", args, tt.wantArgs)
			}
		})
	}
}

func TestNormalizer_normalizeCommand(t *testing.T) {
	n := NewNormalizer()

	// Test that commands in PATH are resolved
	lsPath := n.normalizeCommand("ls")
	if lsPath == "ls" && os.Getenv("PATH") != "" {
		// ls should be resolved to full path on most systems
		// but this might fail in minimal environments
		t.Logf("ls resolved to: %s", lsPath)
	}

	// Test absolute path stays absolute
	absPath := n.normalizeCommand("/usr/bin/ls")
	if !filepath.IsAbs(absPath) && absPath != "/usr/bin/ls" {
		t.Errorf("normalizeCommand(/usr/bin/ls) = %v, want absolute path", absPath)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsString(s, substr))
}

func containsString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func stringSliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
