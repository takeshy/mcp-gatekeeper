package executor

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestNewSandbox(t *testing.T) {
	// Create a temporary directory for testing
	tmpDir, err := os.MkdirTemp("", "sandbox_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	tests := []struct {
		name    string
		config  *SandboxConfig
		wantErr bool
	}{
		{
			name:    "nil config",
			config:  nil,
			wantErr: true,
		},
		{
			name: "empty root dir",
			config: &SandboxConfig{
				Mode:    SandboxAuto,
				RootDir: "",
			},
			wantErr: true,
		},
		{
			name: "valid config with auto mode",
			config: &SandboxConfig{
				Mode:    SandboxAuto,
				RootDir: tmpDir,
			},
			wantErr: false,
		},
		{
			name: "valid config with none mode",
			config: &SandboxConfig{
				Mode:    SandboxNone,
				RootDir: tmpDir,
			},
			wantErr: false,
		},
		{
			name: "nonexistent root dir",
			config: &SandboxConfig{
				Mode:    SandboxNone,
				RootDir: "/nonexistent/path/that/does/not/exist",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewSandbox(tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewSandbox() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestSandbox_Mode(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "sandbox_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Test none mode
	s, err := NewSandbox(&SandboxConfig{
		Mode:    SandboxNone,
		RootDir: tmpDir,
	})
	if err != nil {
		t.Fatalf("NewSandbox() error = %v", err)
	}
	if s.Mode() != SandboxNone {
		t.Errorf("Mode() = %v, want %v", s.Mode(), SandboxNone)
	}
}

func TestSandbox_RootDir(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "sandbox_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	s, err := NewSandbox(&SandboxConfig{
		Mode:    SandboxNone,
		RootDir: tmpDir,
	})
	if err != nil {
		t.Fatalf("NewSandbox() error = %v", err)
	}

	// RootDir should return the absolute path
	if !strings.HasPrefix(s.RootDir(), "/") {
		t.Errorf("RootDir() = %v, want absolute path", s.RootDir())
	}
}

func TestSandbox_ValidatePath(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "sandbox_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create subdirectory
	subDir := tmpDir + "/subdir"
	if err := os.Mkdir(subDir, 0755); err != nil {
		t.Fatalf("Failed to create subdir: %v", err)
	}

	s, err := NewSandbox(&SandboxConfig{
		Mode:    SandboxNone,
		RootDir: tmpDir,
	})
	if err != nil {
		t.Fatalf("NewSandbox() error = %v", err)
	}

	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{
			name:    "root dir itself",
			path:    tmpDir,
			wantErr: false,
		},
		{
			name:    "path within root",
			path:    subDir,
			wantErr: false,
		},
		{
			name:    "path outside root",
			path:    "/tmp",
			wantErr: true,
		},
		{
			name:    "parent directory escape",
			path:    tmpDir + "/..",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := s.ValidatePath(tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidatePath(%q) error = %v, wantErr %v", tt.path, err, tt.wantErr)
			}
		})
	}
}

func TestSandbox_WrapCommand_NoneMode(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "sandbox_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	s, err := NewSandbox(&SandboxConfig{
		Mode:    SandboxNone,
		RootDir: tmpDir,
	})
	if err != nil {
		t.Fatalf("NewSandbox() error = %v", err)
	}

	// Test wrapping a command in none mode
	cmd, args, err := s.WrapCommand(tmpDir, "echo", []string{"hello"})
	if err != nil {
		t.Fatalf("WrapCommand() error = %v", err)
	}

	// In none mode, command should be unchanged
	if cmd != "echo" {
		t.Errorf("WrapCommand() cmd = %v, want echo", cmd)
	}
	if len(args) != 1 || args[0] != "hello" {
		t.Errorf("WrapCommand() args = %v, want [hello]", args)
	}
}

func TestSandbox_WrapCommand_InvalidPath(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "sandbox_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	s, err := NewSandbox(&SandboxConfig{
		Mode:    SandboxNone,
		RootDir: tmpDir,
	})
	if err != nil {
		t.Fatalf("NewSandbox() error = %v", err)
	}

	// Test wrapping with invalid cwd
	_, _, err = s.WrapCommand("/tmp", "echo", []string{"hello"})
	if err == nil {
		t.Errorf("WrapCommand() should fail for path outside root")
	}
}

func TestSandbox_IsBwrapAvailable(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "sandbox_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Check if bwrap is actually available on this system
	_, bwrapErr := exec.LookPath("bwrap")
	bwrapInstalled := bwrapErr == nil

	s, err := NewSandbox(&SandboxConfig{
		Mode:    SandboxAuto,
		RootDir: tmpDir,
	})
	if err != nil {
		t.Fatalf("NewSandbox() error = %v", err)
	}

	if s.IsBwrapAvailable() != bwrapInstalled {
		t.Errorf("IsBwrapAvailable() = %v, expected %v (bwrap installed: %v)",
			s.IsBwrapAvailable(), bwrapInstalled, bwrapInstalled)
	}
}

func TestSandbox_WrapCommand_BwrapMode(t *testing.T) {
	// Skip if bwrap is not installed
	if _, err := exec.LookPath("bwrap"); err != nil {
		t.Skip("bwrap not installed, skipping test")
	}

	tmpDir, err := os.MkdirTemp("", "sandbox_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	s, err := NewSandbox(&SandboxConfig{
		Mode:    SandboxBwrap,
		RootDir: tmpDir,
	})
	if err != nil {
		t.Fatalf("NewSandbox() error = %v", err)
	}

	if s.Mode() != SandboxBwrap {
		t.Skipf("bwrap mode not active (mode=%v), skipping", s.Mode())
	}

	cmd, args, err := s.WrapCommand(tmpDir, "echo", []string{"hello"})
	if err != nil {
		t.Fatalf("WrapCommand() error = %v", err)
	}

	// Command should be bwrap
	if !strings.HasSuffix(cmd, "bwrap") {
		t.Errorf("WrapCommand() cmd = %v, want bwrap", cmd)
	}

	// Args should contain the original command
	found := false
	for i, arg := range args {
		if arg == "echo" && i < len(args)-1 && args[i+1] == "hello" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("WrapCommand() args should contain 'echo hello', got %v", args)
	}

	// Args should contain --chdir with the cwd
	foundChdir := false
	for i, arg := range args {
		if arg == "--chdir" && i+1 < len(args) && args[i+1] == tmpDir {
			foundChdir = true
			break
		}
	}
	if !foundChdir {
		t.Errorf("WrapCommand() args should contain '--chdir %s', got %v", tmpDir, args)
	}
}

func TestSandbox_AutoMode_FallbackToNone(t *testing.T) {
	// This test verifies that auto mode falls back to none when bwrap is not available
	// We can't easily simulate bwrap not being available, so we just verify the behavior

	tmpDir, err := os.MkdirTemp("", "sandbox_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	s, err := NewSandbox(&SandboxConfig{
		Mode:    SandboxAuto,
		RootDir: tmpDir,
	})
	if err != nil {
		t.Fatalf("NewSandbox() error = %v", err)
	}

	// Mode should be either bwrap (if installed) or none (if not)
	mode := s.Mode()
	if mode != SandboxBwrap && mode != SandboxNone {
		t.Errorf("Mode() = %v, want bwrap or none", mode)
	}

	// If bwrap is not available, mode should be none
	if !s.IsBwrapAvailable() && mode != SandboxNone {
		t.Errorf("Mode() = %v when bwrap not available, want none", mode)
	}

	// If bwrap is available, mode should be bwrap
	if s.IsBwrapAvailable() && mode != SandboxBwrap {
		t.Errorf("Mode() = %v when bwrap available, want bwrap", mode)
	}
}
