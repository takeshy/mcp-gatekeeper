package executor

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// SandboxMode represents the sandboxing mode
type SandboxMode string

const (
	// SandboxNone uses only path validation (no real sandboxing)
	SandboxNone SandboxMode = "none"
	// SandboxBwrap uses bubblewrap for sandboxing
	SandboxBwrap SandboxMode = "bwrap"
	// SandboxWasm uses wazero for WASM sandboxing
	SandboxWasm SandboxMode = "wasm"
	// SandboxAuto automatically selects the best available sandbox
	SandboxAuto SandboxMode = "auto"
)

// Sandbox provides command sandboxing functionality
type Sandbox struct {
	mode    SandboxMode
	rootDir string
	bwrap   string // path to bwrap binary
}

// SandboxConfig holds sandbox configuration
type SandboxConfig struct {
	Mode    SandboxMode
	RootDir string
}

// NewSandbox creates a new Sandbox instance
func NewSandbox(config *SandboxConfig) (*Sandbox, error) {
	if config == nil {
		return nil, fmt.Errorf("sandbox config is required")
	}

	if config.RootDir == "" {
		return nil, fmt.Errorf("root directory is required")
	}

	// Resolve root directory to absolute path
	rootDir, err := filepath.Abs(config.RootDir)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve root directory: %w", err)
	}

	// Verify root directory exists
	info, err := os.Stat(rootDir)
	if err != nil {
		return nil, fmt.Errorf("root directory does not exist: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("root path is not a directory: %s", rootDir)
	}

	s := &Sandbox{
		mode:    config.Mode,
		rootDir: rootDir,
	}

	// Determine effective mode
	if s.mode == SandboxAuto || s.mode == SandboxBwrap {
		bwrapPath, err := exec.LookPath("bwrap")
		if err != nil {
			if s.mode == SandboxBwrap {
				// Explicitly requested bwrap but not available
				fmt.Fprintf(os.Stderr, "WARNING: bwrap not found, falling back to path validation only\n")
				fmt.Fprintf(os.Stderr, "WARNING: Commands may access files outside root directory\n")
				fmt.Fprintf(os.Stderr, "WARNING: Install bubblewrap for proper sandboxing: https://github.com/containers/bubblewrap\n")
			}
			s.mode = SandboxNone
		} else {
			s.bwrap = bwrapPath
			s.mode = SandboxBwrap
		}
	}

	return s, nil
}

// Mode returns the effective sandbox mode
func (s *Sandbox) Mode() SandboxMode {
	return s.mode
}

// RootDir returns the sandbox root directory
func (s *Sandbox) RootDir() string {
	return s.rootDir
}

// IsBwrapAvailable returns true if bwrap is available
func (s *Sandbox) IsBwrapAvailable() bool {
	return s.bwrap != ""
}

// WrapCommand wraps a command with sandbox if available
// Returns the command name and arguments to execute
func (s *Sandbox) WrapCommand(cwd, cmd string, args []string) (string, []string, error) {
	// Validate cwd is within root (always, as basic check)
	if err := s.validatePath(cwd); err != nil {
		return "", nil, err
	}

	if s.mode == SandboxBwrap && s.bwrap != "" {
		return s.wrapWithBwrap(cwd, cmd, args)
	}

	// No sandboxing, return as-is
	return cmd, args, nil
}

// wrapWithBwrap creates bwrap command arguments
// rootDir is mounted as / inside the sandbox (same as WASM)
func (s *Sandbox) wrapWithBwrap(cwd, cmd string, args []string) (string, []string, error) {
	// Convert cwd from host path to sandbox path (relative to rootDir -> /)
	sandboxCwd := s.toSandboxPath(cwd)

	// Build bwrap arguments
	// Mount rootDir as / first, then overlay system directories
	bwrapArgs := []string{
		// Mount rootDir as root filesystem
		"--bind", s.rootDir, "/",
		// Read-only bind mounts for system directories (overlay on top of /)
		"--ro-bind", "/usr", "/usr",
		"--ro-bind", "/bin", "/bin",
		"--ro-bind", "/lib", "/lib",
		"--ro-bind", "/lib64", "/lib64",
		// Bind /etc read-only for basic system config
		"--ro-bind", "/etc", "/etc",
		// Create minimal /dev (provides /dev/null, /dev/zero, /dev/urandom, etc.)
		"--dev", "/dev",
		// Create minimal /tmp
		"--tmpfs", "/tmp",
		// Set working directory
		"--chdir", sandboxCwd,
		// Unshare namespaces for isolation
		"--unshare-user",
		"--unshare-pid",
		"--unshare-net",
		"--unshare-uts",
		"--unshare-cgroup",
		// Die when parent dies
		"--die-with-parent",
		// Create new session
		"--new-session",
	}

	// Check if /lib64 exists (some systems don't have it)
	if _, err := os.Stat("/lib64"); os.IsNotExist(err) {
		// Remove --ro-bind /lib64 /lib64 from args
		newArgs := make([]string, 0, len(bwrapArgs)-2)
		for i := 0; i < len(bwrapArgs); i++ {
			if bwrapArgs[i] == "--ro-bind" && i+2 < len(bwrapArgs) && bwrapArgs[i+1] == "/lib64" {
				i += 2 // Skip this bind
				continue
			}
			newArgs = append(newArgs, bwrapArgs[i])
		}
		bwrapArgs = newArgs
	}

	// Add optional directories if they exist (read-only for system tools)
	optionalDirs := []string{"/sbin"}
	for _, dir := range optionalDirs {
		if _, err := os.Stat(dir); err == nil {
			bwrapArgs = append(bwrapArgs, "--ro-bind", dir, dir)
		}
	}

	// Add the actual command and its arguments
	bwrapArgs = append(bwrapArgs, cmd)
	bwrapArgs = append(bwrapArgs, args...)

	return s.bwrap, bwrapArgs, nil
}

// toSandboxPath converts a host path to the corresponding sandbox path
// Host rootDir maps to / inside the sandbox
func (s *Sandbox) toSandboxPath(hostPath string) string {
	if strings.HasPrefix(hostPath, s.rootDir) {
		relative := strings.TrimPrefix(hostPath, s.rootDir)
		if relative == "" || relative == "/" {
			return "/"
		}
		return relative
	}
	return hostPath
}

// validatePath checks if a path is within the root directory
func (s *Sandbox) validatePath(path string) error {
	// Resolve to absolute path
	pathAbs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("failed to resolve path: %w", err)
	}

	// Evaluate symlinks for path
	pathReal, err := filepath.EvalSymlinks(pathAbs)
	if err != nil {
		// Path might not exist yet, use absolute path
		pathReal = pathAbs
	}

	// Evaluate symlinks for root
	rootReal, err := filepath.EvalSymlinks(s.rootDir)
	if err != nil {
		return fmt.Errorf("failed to evaluate root dir symlinks: %w", err)
	}

	// Check if path is within root
	if !IsPathWithinRoot(rootReal, pathReal) {
		return fmt.Errorf("path %q is outside root directory %q", path, s.rootDir)
	}

	return nil
}

// ValidatePath is a public wrapper for path validation
func (s *Sandbox) ValidatePath(path string) error {
	return s.validatePath(path)
}
