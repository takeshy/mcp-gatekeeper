package policy

import (
	"fmt"
	"sync"

	"github.com/gobwas/glob"
)

// Matcher handles glob pattern matching with caching
type Matcher struct {
	cache sync.Map // map[string]glob.Glob
}

// NewMatcher creates a new Matcher
func NewMatcher() *Matcher {
	return &Matcher{}
}

// Compile compiles a glob pattern (with caching)
func (m *Matcher) Compile(pattern string) (glob.Glob, error) {
	if cached, ok := m.cache.Load(pattern); ok {
		return cached.(glob.Glob), nil
	}

	g, err := glob.Compile(pattern, '/')
	if err != nil {
		return nil, fmt.Errorf("invalid glob pattern %q: %w", pattern, err)
	}

	m.cache.Store(pattern, g)
	return g, nil
}

// Match checks if a value matches a pattern
func (m *Matcher) Match(pattern, value string) (bool, error) {
	g, err := m.Compile(pattern)
	if err != nil {
		return false, err
	}
	return g.Match(value), nil
}

// MatchAny checks if a value matches any of the patterns
func (m *Matcher) MatchAny(patterns []string, value string) (matched bool, matchedPattern string, err error) {
	for _, pattern := range patterns {
		matches, err := m.Match(pattern, value)
		if err != nil {
			return false, "", err
		}
		if matches {
			return true, pattern, nil
		}
	}
	return false, "", nil
}

// MatchAll checks if a value matches all patterns
func (m *Matcher) MatchAll(patterns []string, value string) (bool, error) {
	for _, pattern := range patterns {
		matches, err := m.Match(pattern, value)
		if err != nil {
			return false, err
		}
		if !matches {
			return false, nil
		}
	}
	return true, nil
}

// MatchCwd checks if a cwd is allowed by any of the allowed patterns
func (m *Matcher) MatchCwd(allowedCwdGlobs []string, cwd string) (allowed bool, matchedPattern string, err error) {
	if len(allowedCwdGlobs) == 0 {
		// No restrictions - allow all
		return true, "", nil
	}
	return m.MatchAny(allowedCwdGlobs, cwd)
}

// MatchCommand checks if a command matches against allowed/denied patterns
// Returns: (allowed, matchedPattern, isDenyMatch, error)
func (m *Matcher) MatchCommand(allowedCmdGlobs, deniedCmdGlobs []string, cmdline string) (allowed bool, matchedPattern string, isDenyMatch bool, err error) {
	// Check denied patterns first
	if denyMatched, pattern, err := m.MatchAny(deniedCmdGlobs, cmdline); err != nil {
		return false, "", false, err
	} else if denyMatched {
		return false, pattern, true, nil
	}

	// Check allowed patterns
	if len(allowedCmdGlobs) == 0 {
		// No allow restrictions - allow all (if not denied)
		return true, "", false, nil
	}

	if allowMatched, pattern, err := m.MatchAny(allowedCmdGlobs, cmdline); err != nil {
		return false, "", false, err
	} else if allowMatched {
		return true, pattern, false, nil
	}

	// Not in allow list
	return false, "", false, nil
}
