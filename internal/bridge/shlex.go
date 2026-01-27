package bridge

import (
	"errors"
	"strings"
)

// ErrUnterminatedQuote is returned when a quoted string is not properly closed
var ErrUnterminatedQuote = errors.New("unterminated quote")

// ParseCommand parses a shell-like command string into command and arguments.
// It handles single quotes, double quotes, and escaped characters.
// Examples:
//   - `node server.js` -> ["node", "server.js"]
//   - `node --arg "foo bar"` -> ["node", "--arg", "foo bar"]
//   - `node --arg 'foo bar'` -> ["node", "--arg", "foo bar"]
//   - `node --arg "foo \"bar\""` -> ["node", "--arg", `foo "bar"`]
func ParseCommand(input string) ([]string, error) {
	var result []string
	var current strings.Builder
	var inSingleQuote, inDoubleQuote, escaped bool

	for i := 0; i < len(input); i++ {
		c := input[i]

		if escaped {
			current.WriteByte(c)
			escaped = false
			continue
		}

		switch c {
		case '\\':
			if inSingleQuote {
				// Backslash is literal in single quotes
				current.WriteByte(c)
			} else {
				escaped = true
			}
		case '\'':
			if inDoubleQuote {
				current.WriteByte(c)
			} else {
				inSingleQuote = !inSingleQuote
			}
		case '"':
			if inSingleQuote {
				current.WriteByte(c)
			} else {
				inDoubleQuote = !inDoubleQuote
			}
		case ' ', '\t':
			if inSingleQuote || inDoubleQuote {
				current.WriteByte(c)
			} else if current.Len() > 0 {
				result = append(result, current.String())
				current.Reset()
			}
		default:
			current.WriteByte(c)
		}
	}

	if inSingleQuote || inDoubleQuote {
		return nil, ErrUnterminatedQuote
	}

	if current.Len() > 0 {
		result = append(result, current.String())
	}

	return result, nil
}
