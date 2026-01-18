package policy

import (
	"testing"
)

func TestMatcher_Match(t *testing.T) {
	m := NewMatcher()

	tests := []struct {
		name    string
		pattern string
		value   string
		want    bool
	}{
		// Basic patterns
		{"exact match", "foo", "foo", true},
		{"exact no match", "foo", "bar", false},

		// Wildcard patterns
		{"star match", "*.txt", "file.txt", true},
		{"star no match", "*.txt", "file.md", false},
		{"double star", "**/file.txt", "a/b/file.txt", true},
		{"double star root", "file.txt", "file.txt", true}, // Direct match

		// Path patterns
		{"path match", "/home/*/projects", "/home/user/projects", true},
		{"path no match", "/home/*/projects", "/home/user/other", false},
		{"path prefix", "/home/**", "/home/user/projects/file", true},

		// Command patterns
		{"cmd exact", "/usr/bin/ls", "/usr/bin/ls", true},
		{"cmd glob", "/usr/bin/*", "/usr/bin/cat", true},
		{"cmd pattern", "/usr/bin/git *", "/usr/bin/git status", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := m.Match(tt.pattern, tt.value)
			if err != nil {
				t.Errorf("Match() error = %v", err)
				return
			}
			if got != tt.want {
				t.Errorf("Match(%q, %q) = %v, want %v", tt.pattern, tt.value, got, tt.want)
			}
		})
	}
}

func TestMatcher_MatchAny(t *testing.T) {
	m := NewMatcher()

	tests := []struct {
		name         string
		patterns     []string
		value        string
		wantMatched  bool
		wantPattern  string
	}{
		{
			name:        "empty patterns",
			patterns:    []string{},
			value:       "anything",
			wantMatched: false,
		},
		{
			name:         "first match",
			patterns:     []string{"foo", "bar"},
			value:        "foo",
			wantMatched:  true,
			wantPattern:  "foo",
		},
		{
			name:         "second match",
			patterns:     []string{"foo", "bar"},
			value:        "bar",
			wantMatched:  true,
			wantPattern:  "bar",
		},
		{
			name:        "no match",
			patterns:    []string{"foo", "bar"},
			value:       "baz",
			wantMatched: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matched, pattern, err := m.MatchAny(tt.patterns, tt.value)
			if err != nil {
				t.Errorf("MatchAny() error = %v", err)
				return
			}
			if matched != tt.wantMatched {
				t.Errorf("MatchAny() matched = %v, want %v", matched, tt.wantMatched)
			}
			if pattern != tt.wantPattern {
				t.Errorf("MatchAny() pattern = %v, want %v", pattern, tt.wantPattern)
			}
		})
	}
}

func TestMatcher_MatchCwd(t *testing.T) {
	m := NewMatcher()

	tests := []struct {
		name           string
		allowedGlobs   []string
		cwd            string
		wantAllowed    bool
		wantPattern    string
	}{
		{
			name:         "empty allowed allows all",
			allowedGlobs: []string{},
			cwd:          "/any/path",
			wantAllowed:  true,
		},
		{
			name:         "allowed match",
			allowedGlobs: []string{"/home/**"},
			cwd:          "/home/user/projects",
			wantAllowed:  true,
			wantPattern:  "/home/**",
		},
		{
			name:         "not allowed",
			allowedGlobs: []string{"/home/**"},
			cwd:          "/var/lib",
			wantAllowed:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			allowed, pattern, err := m.MatchCwd(tt.allowedGlobs, tt.cwd)
			if err != nil {
				t.Errorf("MatchCwd() error = %v", err)
				return
			}
			if allowed != tt.wantAllowed {
				t.Errorf("MatchCwd() allowed = %v, want %v", allowed, tt.wantAllowed)
			}
			if pattern != tt.wantPattern {
				t.Errorf("MatchCwd() pattern = %v, want %v", pattern, tt.wantPattern)
			}
		})
	}
}
