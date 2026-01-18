package policy

import (
	"testing"

	"github.com/takeshy/mcp-gatekeeper/internal/db"
)

func TestEvaluator_Evaluate_DenyOverrides(t *testing.T) {
	e := NewEvaluator()

	tests := []struct {
		name        string
		policy      *db.Policy
		req         *EvaluateRequest
		wantAllowed bool
	}{
		{
			name: "allow all when no rules",
			policy: &db.Policy{
				Precedence:      db.PrecedenceDenyOverrides,
				AllowedCwdGlobs: []string{},
				AllowedCmdGlobs: []string{},
				DeniedCmdGlobs:  []string{},
			},
			req: &EvaluateRequest{
				Cwd:     "/home/user",
				Cmdline: "ls -la",
			},
			wantAllowed: true,
		},
		{
			name: "deny by cwd",
			policy: &db.Policy{
				Precedence:      db.PrecedenceDenyOverrides,
				AllowedCwdGlobs: []string{"/home/**"},
				AllowedCmdGlobs: []string{},
				DeniedCmdGlobs:  []string{},
			},
			req: &EvaluateRequest{
				Cwd:     "/var/lib",
				Cmdline: "ls -la",
			},
			wantAllowed: false,
		},
		{
			name: "allow by cwd",
			policy: &db.Policy{
				Precedence:      db.PrecedenceDenyOverrides,
				AllowedCwdGlobs: []string{"/home/**"},
				AllowedCmdGlobs: []string{},
				DeniedCmdGlobs:  []string{},
			},
			req: &EvaluateRequest{
				Cwd:     "/home/user",
				Cmdline: "ls -la",
			},
			wantAllowed: true,
		},
		{
			name: "deny by cmd pattern",
			policy: &db.Policy{
				Precedence:      db.PrecedenceDenyOverrides,
				AllowedCwdGlobs: []string{},
				AllowedCmdGlobs: []string{"*"},
				DeniedCmdGlobs:  []string{"rm *"},
			},
			req: &EvaluateRequest{
				Cwd:     "/home/user",
				Cmdline: "rm -rf /",
			},
			wantAllowed: false,
		},
		{
			name: "allow by cmd pattern",
			policy: &db.Policy{
				Precedence:      db.PrecedenceDenyOverrides,
				AllowedCwdGlobs: []string{},
				AllowedCmdGlobs: []string{"ls *", "cat *"},
				DeniedCmdGlobs:  []string{},
			},
			req: &EvaluateRequest{
				Cwd:     "/home/user",
				Cmdline: "ls -la",
			},
			wantAllowed: true,
		},
		{
			name: "deny overrides allow",
			policy: &db.Policy{
				Precedence:      db.PrecedenceDenyOverrides,
				AllowedCwdGlobs: []string{},
				AllowedCmdGlobs: []string{"*"},
				DeniedCmdGlobs:  []string{"rm *"},
			},
			req: &EvaluateRequest{
				Cwd:     "/home/user",
				Cmdline: "rm file.txt",
			},
			wantAllowed: false,
		},
		{
			name: "not in allow list",
			policy: &db.Policy{
				Precedence:      db.PrecedenceDenyOverrides,
				AllowedCwdGlobs: []string{},
				AllowedCmdGlobs: []string{"ls *", "cat *"},
				DeniedCmdGlobs:  []string{},
			},
			req: &EvaluateRequest{
				Cwd:     "/home/user",
				Cmdline: "rm file.txt",
			},
			wantAllowed: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decision, err := e.Evaluate(tt.policy, tt.req)
			if err != nil {
				t.Errorf("Evaluate() error = %v", err)
				return
			}
			if decision.Allowed != tt.wantAllowed {
				t.Errorf("Evaluate() allowed = %v, want %v (reason: %s)", decision.Allowed, tt.wantAllowed, decision.Reason)
			}
		})
	}
}

func TestEvaluator_Evaluate_AllowOverrides(t *testing.T) {
	e := NewEvaluator()

	tests := []struct {
		name        string
		policy      *db.Policy
		req         *EvaluateRequest
		wantAllowed bool
	}{
		{
			name: "allow overrides deny",
			policy: &db.Policy{
				Precedence:      db.PrecedenceAllowOverrides,
				AllowedCwdGlobs: []string{},
				AllowedCmdGlobs: []string{"rm -i *"},
				DeniedCmdGlobs:  []string{"rm *"},
			},
			req: &EvaluateRequest{
				Cwd:     "/home/user",
				Cmdline: "rm -i file.txt",
			},
			wantAllowed: true,
		},
		{
			name: "denied when not in allow",
			policy: &db.Policy{
				Precedence:      db.PrecedenceAllowOverrides,
				AllowedCwdGlobs: []string{},
				AllowedCmdGlobs: []string{"rm -i *"},
				DeniedCmdGlobs:  []string{"rm *"},
			},
			req: &EvaluateRequest{
				Cwd:     "/home/user",
				Cmdline: "rm -rf /",
			},
			wantAllowed: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decision, err := e.Evaluate(tt.policy, tt.req)
			if err != nil {
				t.Errorf("Evaluate() error = %v", err)
				return
			}
			if decision.Allowed != tt.wantAllowed {
				t.Errorf("Evaluate() allowed = %v, want %v (reason: %s)", decision.Allowed, tt.wantAllowed, decision.Reason)
			}
		})
	}
}

func TestValidatePolicy(t *testing.T) {
	tests := []struct {
		name    string
		policy  *db.Policy
		wantErr bool
	}{
		{
			name: "valid policy",
			policy: &db.Policy{
				AllowedCwdGlobs: []string{"/home/**"},
				AllowedCmdGlobs: []string{"ls *"},
				DeniedCmdGlobs:  []string{"rm *"},
				AllowedEnvKeys:  []string{"PATH", "HOME"},
			},
			wantErr: false,
		},
		{
			name: "invalid cwd glob",
			policy: &db.Policy{
				AllowedCwdGlobs: []string{"[invalid"},
			},
			wantErr: true,
		},
		{
			name: "invalid cmd glob",
			policy: &db.Policy{
				AllowedCmdGlobs: []string{"[invalid"},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePolicy(tt.policy)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidatePolicy() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
