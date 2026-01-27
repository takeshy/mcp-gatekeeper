package bridge

import (
	"reflect"
	"testing"
)

func TestParseCommand(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    []string
		wantErr bool
	}{
		{
			name:  "simple command",
			input: "node server.js",
			want:  []string{"node", "server.js"},
		},
		{
			name:  "single argument",
			input: "echo",
			want:  []string{"echo"},
		},
		{
			name:  "double quoted argument",
			input: `node --arg "foo bar"`,
			want:  []string{"node", "--arg", "foo bar"},
		},
		{
			name:  "single quoted argument",
			input: `node --arg 'foo bar'`,
			want:  []string{"node", "--arg", "foo bar"},
		},
		{
			name:  "escaped double quote",
			input: `node --arg "foo \"bar\""`,
			want:  []string{"node", "--arg", `foo "bar"`},
		},
		{
			name:  "multiple quoted arguments",
			input: `cmd "arg 1" "arg 2"`,
			want:  []string{"cmd", "arg 1", "arg 2"},
		},
		{
			name:  "mixed quotes",
			input: `cmd "double" 'single'`,
			want:  []string{"cmd", "double", "single"},
		},
		{
			name:  "empty string",
			input: "",
			want:  nil,
		},
		{
			name:  "only spaces",
			input: "   ",
			want:  nil,
		},
		{
			name:  "tabs as separators",
			input: "cmd\targ1\targ2",
			want:  []string{"cmd", "arg1", "arg2"},
		},
		{
			name:  "multiple spaces",
			input: "cmd   arg1    arg2",
			want:  []string{"cmd", "arg1", "arg2"},
		},
		{
			name:  "backslash in single quotes",
			input: `'path\\to\\file'`,
			want:  []string{`path\\to\\file`},
		},
		{
			name:  "escaped space outside quotes",
			input: `cmd arg\ with\ space`,
			want:  []string{"cmd", "arg with space"},
		},
		{
			name:  "single quote inside double quotes",
			input: `"it's ok"`,
			want:  []string{"it's ok"},
		},
		{
			name:  "double quote inside single quotes",
			input: `'say "hello"'`,
			want:  []string{`say "hello"`},
		},
		{
			name:  "path with spaces",
			input: `node "/path/to/my file.js"`,
			want:  []string{"node", "/path/to/my file.js"},
		},
		{
			name:    "unterminated double quote",
			input:   `cmd "unterminated`,
			wantErr: true,
		},
		{
			name:    "unterminated single quote",
			input:   `cmd 'unterminated`,
			wantErr: true,
		},
		{
			name:  "adjacent quoted strings",
			input: `"foo""bar"`,
			want:  []string{"foobar"},
		},
		{
			name:  "npx command",
			input: `npx @anthropic-ai/mcp-filesystem-server /home/user/projects`,
			want:  []string{"npx", "@anthropic-ai/mcp-filesystem-server", "/home/user/projects"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseCommand(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseCommand() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParseCommand() = %v, want %v", got, tt.want)
			}
		})
	}
}
