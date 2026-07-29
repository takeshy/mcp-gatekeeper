package oauth

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

var dummyBcryptHash = []byte("$2y$10$7EqJtq98hPqEX7fNZaFWoO5uB8R6V6YF4d/SZoD2mB6qM3WfP7Y6K")

// authenticateHTPasswd validates bcrypt-only htpasswd credentials. The file is
// reloaded for each login so revocation and password changes take effect immediately.
func authenticateHTPasswd(path, username, password string) (bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return false, fmt.Errorf("open htpasswd: %w", err)
	}
	defer file.Close()

	var selected []byte
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, hash, ok := strings.Cut(line, ":")
		if !ok {
			return false, fmt.Errorf("invalid htpasswd entry")
		}
		if !strings.HasPrefix(hash, "$2a$") && !strings.HasPrefix(hash, "$2b$") && !strings.HasPrefix(hash, "$2y$") {
			return false, fmt.Errorf("htpasswd contains a non-bcrypt entry for %q", name)
		}
		if name == username {
			selected = []byte(hash)
		}
	}
	if err := scanner.Err(); err != nil {
		return false, fmt.Errorf("read htpasswd: %w", err)
	}
	if selected == nil {
		_ = bcrypt.CompareHashAndPassword(dummyBcryptHash, []byte(password))
		return false, nil
	}
	return bcrypt.CompareHashAndPassword(selected, []byte(password)) == nil, nil
}
