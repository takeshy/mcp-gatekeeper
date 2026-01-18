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

// Evaluator evaluates tools against requests
type Evaluator struct {
	matcher *Matcher
}

// NewEvaluator creates a new Evaluator
func NewEvaluator() *Evaluator {
	return &Evaluator{
		matcher: NewMatcher(),
	}
}

// EvaluateArgs evaluates if the given arguments are allowed for a tool
func (e *Evaluator) EvaluateArgs(tool *db.Tool, args []string) (*Decision, error) {
	decision := &Decision{
		MatchedRules: make([]string, 0),
	}

	// Join arguments for matching
	cmdline := strings.Join(args, " ")

	// If AllowedArgGlobs is empty, allow all arguments
	if len(tool.AllowedArgGlobs) == 0 {
		decision.Allowed = true
		decision.Reason = "allowed (no argument restrictions)"
		return decision, nil
	}

	// Check if any pattern matches
	for _, pattern := range tool.AllowedArgGlobs {
		matched, err := e.matcher.Match(pattern, cmdline)
		if err != nil {
			return nil, fmt.Errorf("failed to match pattern %q: %w", pattern, err)
		}
		if matched {
			decision.Allowed = true
			decision.Reason = fmt.Sprintf("allowed by pattern %q", pattern)
			decision.MatchedRules = append(decision.MatchedRules, fmt.Sprintf("arg_allow:%s", pattern))
			return decision, nil
		}
	}

	// No pattern matched
	decision.Allowed = false
	decision.Reason = "arguments not in allowed patterns"
	return decision, nil
}

// FilterEnvKeys filters environment variable keys based on allowed patterns
func (e *Evaluator) FilterEnvKeys(allowedEnvKeys []string, envKeys []string) []string {
	if len(allowedEnvKeys) == 0 {
		return envKeys // No restrictions
	}

	var filtered []string
	for _, key := range envKeys {
		for _, pattern := range allowedEnvKeys {
			matched, _ := e.matcher.Match(pattern, key)
			if matched {
				filtered = append(filtered, key)
				break
			}
		}
	}
	return filtered
}

// ValidateTool validates a tool configuration
func ValidateTool(tool *db.Tool) error {
	matcher := NewMatcher()

	// Validate glob patterns
	for _, pattern := range tool.AllowedArgGlobs {
		if _, err := matcher.Compile(pattern); err != nil {
			return fmt.Errorf("invalid allowed_arg_glob %q: %w", pattern, err)
		}
	}

	// Validate sandbox type
	switch tool.Sandbox {
	case db.SandboxTypeNone, db.SandboxTypeBubblewrap, db.SandboxTypeWasm:
		// Valid
	default:
		return fmt.Errorf("invalid sandbox type %q", tool.Sandbox)
	}

	// Validate wasm binary path for wasm sandbox
	if tool.Sandbox == db.SandboxTypeWasm && tool.WasmBinary == "" {
		return fmt.Errorf("wasm_binary is required for wasm sandbox")
	}

	return nil
}

// ValidateAllowedEnvKeys validates allowed env key patterns
func ValidateAllowedEnvKeys(patterns []string) error {
	matcher := NewMatcher()

	for _, pattern := range patterns {
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
