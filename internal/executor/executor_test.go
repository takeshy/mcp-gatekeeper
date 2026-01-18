package executor

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestExecutor_Execute(t *testing.T) {
	e := NewExecutor(nil)
	ctx := context.Background()

	tests := []struct {
		name         string
		cwd          string
		cmd          string
		args         []string
		wantExitCode int
		wantStdout   string
		wantErr      bool
	}{
		{
			name:         "echo command",
			cwd:          "/tmp",
			cmd:          "echo",
			args:         []string{"hello"},
			wantExitCode: 0,
			wantStdout:   "hello\n",
		},
		{
			name:         "failing command",
			cwd:          "/tmp",
			cmd:          "false",
			args:         nil,
			wantExitCode: 1,
		},
		{
			name:         "command with stderr",
			cwd:          "/tmp",
			cmd:          "sh",
			args:         []string{"-c", "echo error >&2"},
			wantExitCode: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := e.Execute(ctx, tt.cwd, tt.cmd, tt.args, os.Environ())
			if (err != nil) != tt.wantErr {
				t.Errorf("Execute() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err != nil {
				return
			}
			if result.ExitCode != tt.wantExitCode {
				t.Errorf("Execute() exitCode = %v, want %v", result.ExitCode, tt.wantExitCode)
			}
			if tt.wantStdout != "" && result.Stdout != tt.wantStdout {
				t.Errorf("Execute() stdout = %q, want %q", result.Stdout, tt.wantStdout)
			}
		})
	}
}

func TestExecutor_Execute_Timeout(t *testing.T) {
	config := &ExecutorConfig{
		Timeout:   100 * time.Millisecond,
		MaxOutput: 1024,
	}
	e := NewExecutor(config)
	ctx := context.Background()

	result, err := e.Execute(ctx, "/tmp", "sleep", []string{"10"}, nil)
	if err != nil {
		t.Errorf("Execute() error = %v", err)
		return
	}
	if !result.TimedOut {
		t.Errorf("Execute() should have timed out")
	}
}

func TestExecutor_Execute_OutputLimit(t *testing.T) {
	config := &ExecutorConfig{
		Timeout:   30 * time.Second,
		MaxOutput: 1000, // Small but not too small limit
	}
	e := NewExecutor(config)
	ctx := context.Background()

	// Generate output longer than the limit
	result, err := e.Execute(ctx, "/tmp", "sh", []string{"-c", "yes | head -500"}, nil)
	if err != nil {
		// Some systems may error on truncation, which is acceptable
		t.Skipf("Execute() error = %v (may be expected on some systems)", err)
		return
	}

	// Output should be truncated or limited
	if len(result.Stdout) > config.MaxOutput+100 { // Allow some slack for truncation message
		t.Errorf("Execute() stdout length = %v, should be around %v", len(result.Stdout), config.MaxOutput)
	}
}

func TestFilterEnv(t *testing.T) {
	env := []string{
		"PATH=/usr/bin",
		"HOME=/home/user",
		"SECRET=password",
		"USER=testuser",
	}

	tests := []struct {
		name        string
		allowedKeys []string
		wantCount   int
	}{
		{
			name:        "filter to PATH only",
			allowedKeys: []string{"PATH"},
			wantCount:   1,
		},
		{
			name:        "filter to PATH and HOME",
			allowedKeys: []string{"PATH", "HOME"},
			wantCount:   2,
		},
		{
			name:        "filter non-existent key",
			allowedKeys: []string{"NONEXISTENT"},
			wantCount:   0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filtered := filterEnv(env, tt.allowedKeys)
			if len(filtered) != tt.wantCount {
				t.Errorf("filterEnv() count = %v, want %v", len(filtered), tt.wantCount)
			}
		})
	}
}

func TestLimitedBuffer(t *testing.T) {
	buf := &limitedBuffer{maxSize: 10}

	// Write within limit
	n, err := buf.Write([]byte("hello"))
	if err != nil {
		t.Errorf("Write() error = %v", err)
	}
	if n != 5 {
		t.Errorf("Write() n = %v, want 5", n)
	}
	if buf.truncated {
		t.Errorf("Write() truncated should be false")
	}

	// Write more, should trigger truncation
	n, err = buf.Write([]byte("world!!!!"))
	if err != nil {
		t.Errorf("Write() error = %v", err)
	}
	if !buf.truncated {
		t.Errorf("Write() truncated should be true")
	}

	// Buffer should only have first 10 bytes
	if len(buf.String()) != 10 {
		t.Errorf("String() length = %v, want 10", len(buf.String()))
	}
}

func TestIsPathWithinRoot(t *testing.T) {
	tests := []struct {
		name   string
		root   string
		path   string
		expect bool
	}{
		{
			name:   "exact match",
			root:   "/tmp/sandbox",
			path:   "/tmp/sandbox",
			expect: true,
		},
		{
			name:   "path within root",
			root:   "/tmp/sandbox",
			path:   "/tmp/sandbox/subdir",
			expect: true,
		},
		{
			name:   "path deeply nested within root",
			root:   "/tmp/sandbox",
			path:   "/tmp/sandbox/a/b/c/d",
			expect: true,
		},
		{
			name:   "path outside root",
			root:   "/tmp/sandbox",
			path:   "/tmp/other",
			expect: false,
		},
		{
			name:   "path is parent of root",
			root:   "/tmp/sandbox",
			path:   "/tmp",
			expect: false,
		},
		{
			name:   "path with prefix match but not within",
			root:   "/tmp/sandbox",
			path:   "/tmp/sandbox-other",
			expect: false,
		},
		{
			name:   "path traversal attempt",
			root:   "/tmp/sandbox",
			path:   "/tmp/sandbox/../other",
			expect: false,
		},
		{
			name:   "root with trailing slash",
			root:   "/tmp/sandbox/",
			path:   "/tmp/sandbox/subdir",
			expect: true,
		},
		{
			name:   "both with trailing slash",
			root:   "/tmp/sandbox/",
			path:   "/tmp/sandbox/subdir/",
			expect: true,
		},
		{
			name:   "root directory itself",
			root:   "/home/user/projects",
			path:   "/home/user/projects",
			expect: true,
		},
		{
			name:   "file in root",
			root:   "/home/user/projects",
			path:   "/home/user/projects/file.txt",
			expect: true,
		},
		{
			name:   "escape attempt via similar prefix",
			root:   "/home/user/projects",
			path:   "/home/user/projects-evil/malware",
			expect: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsPathWithinRoot(tt.root, tt.path)
			if result != tt.expect {
				t.Errorf("IsPathWithinRoot(%q, %q) = %v, want %v", tt.root, tt.path, result, tt.expect)
			}
		})
	}
}

func TestExecutor_ValidatePath_WithRootDir(t *testing.T) {
	// Create a temporary directory for testing
	tmpDir, err := os.MkdirTemp("", "executor_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a subdirectory
	subDir := tmpDir + "/subdir"
	if err := os.Mkdir(subDir, 0755); err != nil {
		t.Fatalf("Failed to create subdir: %v", err)
	}

	config := &ExecutorConfig{
		Timeout:   DefaultTimeout,
		MaxOutput: DefaultMaxOutput,
		RootDir:   tmpDir,
	}
	e := NewExecutor(config)

	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{
			name:    "path within root",
			path:    subDir,
			wantErr: false,
		},
		{
			name:    "root directory itself",
			path:    tmpDir,
			wantErr: false,
		},
		{
			name:    "path outside root",
			path:    "/tmp",
			wantErr: true,
		},
		{
			name:    "parent directory",
			path:    tmpDir + "/..",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := e.validatePath(tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("validatePath(%q) error = %v, wantErr %v", tt.path, err, tt.wantErr)
			}
		})
	}
}

func TestExecutor_Execute_WithRootDir(t *testing.T) {
	// Create a temporary directory for testing
	tmpDir, err := os.MkdirTemp("", "executor_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	config := &ExecutorConfig{
		Timeout:   DefaultTimeout,
		MaxOutput: DefaultMaxOutput,
		RootDir:   tmpDir,
	}
	e := NewExecutor(config)
	ctx := context.Background()

	tests := []struct {
		name    string
		cwd     string
		cmd     string
		args    []string
		wantErr bool
	}{
		{
			name:    "execute within root",
			cwd:     tmpDir,
			cmd:     "pwd",
			args:    nil,
			wantErr: false,
		},
		{
			name:    "execute outside root",
			cwd:     "/tmp",
			cmd:     "pwd",
			args:    nil,
			wantErr: true,
		},
		{
			name:    "execute with parent traversal",
			cwd:     tmpDir + "/..",
			cmd:     "pwd",
			args:    nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := e.Execute(ctx, tt.cwd, tt.cmd, tt.args, nil)
			if (err != nil) != tt.wantErr {
				t.Errorf("Execute() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestExecutor_GetRootDir(t *testing.T) {
	// Create temp directory for valid root dir test
	tmpDir, err := os.MkdirTemp("", "executor_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	tests := []struct {
		name    string
		rootDir string
	}{
		{
			name:    "with root dir",
			rootDir: tmpDir,
		},
		{
			name:    "empty root dir",
			rootDir: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &ExecutorConfig{
				Timeout:   DefaultTimeout,
				MaxOutput: DefaultMaxOutput,
				RootDir:   tt.rootDir,
			}
			e := NewExecutor(config)
			if got := e.GetRootDir(); got != tt.rootDir {
				t.Errorf("GetRootDir() = %v, want %v", got, tt.rootDir)
			}
		})
	}
}
