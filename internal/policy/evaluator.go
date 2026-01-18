package policy

import (
	"fmt"
	"strings"

	"github.com/takeshy/mcp-gatekeeper/internal/db"
)

// Decision represents the result of policy evaluation
type Decision struct {
	Allowed      bool
	MatchedRules []string
	Reason       string
}

// Evaluator evaluates policies against requests
type Evaluator struct {
	matcher *Matcher
}

// NewEvaluator creates a new Evaluator
func NewEvaluator() *Evaluator {
	return &Evaluator{
		matcher: NewMatcher(),
	}
}

// EvaluateRequest represents a command execution request
type EvaluateRequest struct {
	Cwd     string
	Cmdline string
	EnvKeys []string
}

// Evaluate evaluates a policy against a request
func (e *Evaluator) Evaluate(policy *db.Policy, req *EvaluateRequest) (*Decision, error) {
	decision := &Decision{
		MatchedRules: make([]string, 0),
	}

	// Check CWD first
	cwdAllowed, cwdPattern, err := e.matcher.MatchCwd(policy.AllowedCwdGlobs, req.Cwd)
	if err != nil {
		return nil, fmt.Errorf("failed to match cwd: %w", err)
	}
	if !cwdAllowed {
		decision.Allowed = false
		decision.Reason = fmt.Sprintf("cwd %q not in allowed paths", req.Cwd)
		return decision, nil
	}
	if cwdPattern != "" {
		decision.MatchedRules = append(decision.MatchedRules, fmt.Sprintf("cwd_allow:%s", cwdPattern))
	}

	// Check command based on precedence
	switch policy.Precedence {
	case db.PrecedenceDenyOverrides:
		return e.evaluateDenyOverrides(policy, req, decision)
	case db.PrecedenceAllowOverrides:
		return e.evaluateAllowOverrides(policy, req, decision)
	default:
		return nil, fmt.Errorf("unknown precedence: %s", policy.Precedence)
	}
}

// evaluateDenyOverrides: deny rules take precedence over allow rules
func (e *Evaluator) evaluateDenyOverrides(policy *db.Policy, req *EvaluateRequest, decision *Decision) (*Decision, error) {
	// Check denied patterns first
	if denyMatched, pattern, err := e.matcher.MatchAny(policy.DeniedCmdGlobs, req.Cmdline); err != nil {
		return nil, fmt.Errorf("failed to match deny patterns: %w", err)
	} else if denyMatched {
		decision.Allowed = false
		decision.Reason = fmt.Sprintf("command denied by pattern %q", pattern)
		decision.MatchedRules = append(decision.MatchedRules, fmt.Sprintf("cmd_deny:%s", pattern))
		return decision, nil
	}

	// Check allowed patterns
	if len(policy.AllowedCmdGlobs) == 0 {
		// No allow rules = allow all (that aren't denied)
		decision.Allowed = true
		decision.Reason = "allowed (no allow rules, not denied)"
		return decision, nil
	}

	if allowMatched, pattern, err := e.matcher.MatchAny(policy.AllowedCmdGlobs, req.Cmdline); err != nil {
		return nil, fmt.Errorf("failed to match allow patterns: %w", err)
	} else if allowMatched {
		decision.Allowed = true
		decision.Reason = fmt.Sprintf("allowed by pattern %q", pattern)
		decision.MatchedRules = append(decision.MatchedRules, fmt.Sprintf("cmd_allow:%s", pattern))
		return decision, nil
	}

	// Not in allow list
	decision.Allowed = false
	decision.Reason = "command not in allowed patterns"
	return decision, nil
}

// evaluateAllowOverrides: allow rules take precedence over deny rules
func (e *Evaluator) evaluateAllowOverrides(policy *db.Policy, req *EvaluateRequest, decision *Decision) (*Decision, error) {
	// Check allowed patterns first
	if allowMatched, pattern, err := e.matcher.MatchAny(policy.AllowedCmdGlobs, req.Cmdline); err != nil {
		return nil, fmt.Errorf("failed to match allow patterns: %w", err)
	} else if allowMatched {
		decision.Allowed = true
		decision.Reason = fmt.Sprintf("allowed by pattern %q (overrides deny)", pattern)
		decision.MatchedRules = append(decision.MatchedRules, fmt.Sprintf("cmd_allow:%s", pattern))
		return decision, nil
	}

	// Check denied patterns
	if denyMatched, pattern, err := e.matcher.MatchAny(policy.DeniedCmdGlobs, req.Cmdline); err != nil {
		return nil, fmt.Errorf("failed to match deny patterns: %w", err)
	} else if denyMatched {
		decision.Allowed = false
		decision.Reason = fmt.Sprintf("command denied by pattern %q", pattern)
		decision.MatchedRules = append(decision.MatchedRules, fmt.Sprintf("cmd_deny:%s", pattern))
		return decision, nil
	}

	// Not explicitly allowed or denied
	if len(policy.AllowedCmdGlobs) == 0 {
		decision.Allowed = true
		decision.Reason = "allowed (no restrictions)"
		return decision, nil
	}

	decision.Allowed = false
	decision.Reason = "command not in allowed patterns"
	return decision, nil
}

// FilterEnvKeys filters environment variable keys based on allowed patterns
func (e *Evaluator) FilterEnvKeys(policy *db.Policy, envKeys []string) []string {
	if len(policy.AllowedEnvKeys) == 0 {
		return envKeys // No restrictions
	}

	var filtered []string
	for _, key := range envKeys {
		for _, pattern := range policy.AllowedEnvKeys {
			matched, _ := e.matcher.Match(pattern, key)
			if matched {
				filtered = append(filtered, key)
				break
			}
		}
	}
	return filtered
}

// ValidatePolicy validates a policy configuration
func ValidatePolicy(policy *db.Policy) error {
	matcher := NewMatcher()

	// Validate glob patterns
	for _, pattern := range policy.AllowedCwdGlobs {
		if _, err := matcher.Compile(pattern); err != nil {
			return fmt.Errorf("invalid allowed_cwd_glob %q: %w", pattern, err)
		}
	}
	for _, pattern := range policy.AllowedCmdGlobs {
		if _, err := matcher.Compile(pattern); err != nil {
			return fmt.Errorf("invalid allowed_cmd_glob %q: %w", pattern, err)
		}
	}
	for _, pattern := range policy.DeniedCmdGlobs {
		if _, err := matcher.Compile(pattern); err != nil {
			return fmt.Errorf("invalid denied_cmd_glob %q: %w", pattern, err)
		}
	}
	for _, pattern := range policy.AllowedEnvKeys {
		if _, err := matcher.Compile(pattern); err != nil {
			return fmt.Errorf("invalid allowed_env_key %q: %w", pattern, err)
		}
	}

	return nil
}

// FormatDecision formats a decision for display
func FormatDecision(d *Decision) string {
	var sb strings.Builder
	if d.Allowed {
		sb.WriteString("ALLOWED")
	} else {
		sb.WriteString("DENIED")
	}
	sb.WriteString(": ")
	sb.WriteString(d.Reason)
	if len(d.MatchedRules) > 0 {
		sb.WriteString(" [rules: ")
		sb.WriteString(strings.Join(d.MatchedRules, ", "))
		sb.WriteString("]")
	}
	return sb.String()
}
