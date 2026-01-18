package policy

import (
	"testing"

	"github.com/takeshy/mcp-gatekeeper/internal/db"
)

func TestEvaluator_EvaluateArgs(t *testing.T) {
	e := NewEvaluator()

	tests := []struct {
		name        string
		tool        *db.Tool
		args        []string
		wantAllowed bool
	}{
		{
			name: "allow all when no restrictions",
			tool: &db.Tool{
				Name:            "test-tool",
				Command:         "/usr/bin/echo",
				AllowedArgGlobs: []string{},
				Sandbox:         db.SandboxTypeBubblewrap,
			},
			args:        []string{"hello", "world"},
			wantAllowed: true,
		},
		{
			name: "allow when args match pattern",
			tool: &db.Tool{
				Name:            "git-tool",
				Command:         "/usr/bin/git",
				AllowedArgGlobs: []string{"status *", "log *", "diff *"},
				Sandbox:         db.SandboxTypeBubblewrap,
			},
			args:        []string{"status", "--short"},
			wantAllowed: true,
		},
		{
			name: "deny when args don't match pattern",
			tool: &db.Tool{
				Name:            "git-tool",
				Command:         "/usr/bin/git",
				AllowedArgGlobs: []string{"status *", "log *", "diff *"},
				Sandbox:         db.SandboxTypeBubblewrap,
			},
			args:        []string{"push", "origin", "main"},
			wantAllowed: false,
		},
		{
			name: "allow with wildcard pattern",
			tool: &db.Tool{
				Name:            "ls-tool",
				Command:         "/bin/ls",
				AllowedArgGlobs: []string{"**"},
				Sandbox:         db.SandboxTypeBubblewrap,
			},
			args:        []string{"-la", "/tmp"},
			wantAllowed: true,
		},
		{
			name: "allow empty args with empty pattern match",
			tool: &db.Tool{
				Name:            "pwd-tool",
				Command:         "/bin/pwd",
				AllowedArgGlobs: []string{""},
				Sandbox:         db.SandboxTypeBubblewrap,
			},
			args:        []string{},
			wantAllowed: true,
		},
		{
			name: "deny empty args when pattern requires args",
			tool: &db.Tool{
				Name:            "cat-tool",
				Command:         "/bin/cat",
				AllowedArgGlobs: []string{"*.txt"},
				Sandbox:         db.SandboxTypeBubblewrap,
			},
			args:        []string{},
			wantAllowed: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decision, err := e.EvaluateArgs(tt.tool, tt.args)
			if err != nil {
				t.Errorf("EvaluateArgs() error = %v", err)
				return
			}
			if decision.Allowed != tt.wantAllowed {
				t.Errorf("EvaluateArgs() allowed = %v, want %v (reason: %s)", decision.Allowed, tt.wantAllowed, decision.Reason)
			}
		})
	}
}

func TestEvaluator_FilterEnvKeys(t *testing.T) {
	e := NewEvaluator()

	tests := []struct {
		name           string
		allowedEnvKeys []string
		envKeys        []string
		wantCount      int
	}{
		{
			name:           "no restrictions returns all",
			allowedEnvKeys: []string{},
			envKeys:        []string{"PATH", "HOME", "USER"},
			wantCount:      3,
		},
		{
			name:           "filter by exact match",
			allowedEnvKeys: []string{"PATH", "HOME"},
			envKeys:        []string{"PATH", "HOME", "USER", "SHELL"},
			wantCount:      2,
		},
		{
			name:           "filter by wildcard",
			allowedEnvKeys: []string{"*"},
			envKeys:        []string{"PATH", "HOME", "USER"},
			wantCount:      3,
		},
		{
			name:           "filter by prefix wildcard",
			allowedEnvKeys: []string{"GO*"},
			envKeys:        []string{"GOPATH", "GOROOT", "PATH", "HOME"},
			wantCount:      2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := e.FilterEnvKeys(tt.allowedEnvKeys, tt.envKeys)
			if len(result) != tt.wantCount {
				t.Errorf("FilterEnvKeys() returned %d keys, want %d", len(result), tt.wantCount)
			}
		})
	}
}

func TestValidateTool(t *testing.T) {
	tests := []struct {
		name    string
		tool    *db.Tool
		wantErr bool
	}{
		{
			name: "valid tool with bubblewrap",
			tool: &db.Tool{
				Name:            "test-tool",
				Command:         "/usr/bin/test",
				AllowedArgGlobs: []string{"*"},
				Sandbox:         db.SandboxTypeBubblewrap,
			},
			wantErr: false,
		},
		{
			name: "valid tool with none sandbox",
			tool: &db.Tool{
				Name:            "test-tool",
				Command:         "/usr/bin/test",
				AllowedArgGlobs: []string{},
				Sandbox:         db.SandboxTypeNone,
			},
			wantErr: false,
		},
		{
			name: "valid tool with wasm",
			tool: &db.Tool{
				Name:            "wasm-tool",
				Command:         "module",
				AllowedArgGlobs: []string{},
				Sandbox:         db.SandboxTypeWasm,
				WasmBinary:      "/path/to/module.wasm",
			},
			wantErr: false,
		},
		{
			name: "invalid wasm without binary",
			tool: &db.Tool{
				Name:            "wasm-tool",
				Command:         "module",
				AllowedArgGlobs: []string{},
				Sandbox:         db.SandboxTypeWasm,
				WasmBinary:      "",
			},
			wantErr: true,
		},
		{
			name: "invalid arg glob pattern",
			tool: &db.Tool{
				Name:            "test-tool",
				Command:         "/usr/bin/test",
				AllowedArgGlobs: []string{"[invalid"},
				Sandbox:         db.SandboxTypeBubblewrap,
			},
			wantErr: true,
		},
		{
			name: "invalid sandbox type",
			tool: &db.Tool{
				Name:            "test-tool",
				Command:         "/usr/bin/test",
				AllowedArgGlobs: []string{},
				Sandbox:         "invalid",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateTool(tt.tool)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateTool() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateAllowedEnvKeys(t *testing.T) {
	tests := []struct {
		name     string
		patterns []string
		wantErr  bool
	}{
		{
			name:     "valid patterns",
			patterns: []string{"PATH", "HOME", "GO*"},
			wantErr:  false,
		},
		{
			name:     "empty patterns",
			patterns: []string{},
			wantErr:  false,
		},
		{
			name:     "invalid pattern",
			patterns: []string{"[invalid"},
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateAllowedEnvKeys(tt.patterns)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateAllowedEnvKeys() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
