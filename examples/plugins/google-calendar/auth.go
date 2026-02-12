package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/calendar/v3"
)

const defaultCredentialsFile = "credentials.json"
const tokenFile = "token.json"

// configDir returns the configuration directory for mcp-gcal.
func configDir() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "mcp-gcal")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", ".mcp-gcal")
	}
	return filepath.Join(home, ".config", "mcp-gcal")
}

// loadOAuthConfig reads the OAuth2 client credentials from the given file
// (or the default location in configDir).
func loadOAuthConfig(credentialsFile string) (*oauth2.Config, error) {
	if credentialsFile == "" {
		credentialsFile = filepath.Join(configDir(), defaultCredentialsFile)
	}
	b, err := os.ReadFile(credentialsFile)
	if err != nil {
		return nil, fmt.Errorf("unable to read credentials file %s: %w\nDownload it from Google Cloud Console and place it at %s",
			credentialsFile, err, filepath.Join(configDir(), defaultCredentialsFile))
	}
	config, err := google.ConfigFromJSON(b, calendar.CalendarScope)
	if err != nil {
		return nil, fmt.Errorf("unable to parse credentials: %w", err)
	}
	return config, nil
}

// tokenPath returns the path to the stored OAuth2 token.
func tokenPath() string {
	return filepath.Join(configDir(), tokenFile)
}

// loadToken reads a previously saved token from disk.
func loadToken() (*oauth2.Token, error) {
	f, err := os.Open(tokenPath())
	if err != nil {
		return nil, err
	}
	defer f.Close()
	tok := &oauth2.Token{}
	err = json.NewDecoder(f).Decode(tok)
	return tok, err
}

// saveToken saves a token to disk.
func saveToken(token *oauth2.Token) error {
	dir := configDir()
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	f, err := os.OpenFile(tokenPath(), os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	return json.NewEncoder(f).Encode(token)
}

// getTokenSource returns an oauth2.TokenSource that auto-refreshes.
// If no saved token exists, it returns an error instructing the user to run `gcal auth`.
func getTokenSource(config *oauth2.Config) (oauth2.TokenSource, error) {
	tok, err := loadToken()
	if err != nil {
		return nil, fmt.Errorf("no saved token found; run 'gcal auth' first to authenticate")
	}
	ts := config.TokenSource(context.Background(), tok)
	// Try to get a valid token (triggers refresh if needed)
	newTok, err := ts.Token()
	if err != nil {
		return nil, fmt.Errorf("token refresh failed; run 'gcal auth' to re-authenticate: %w", err)
	}
	// Save refreshed token if it changed
	if newTok.AccessToken != tok.AccessToken {
		_ = saveToken(newTok)
	}
	return ts, nil
}

// runAuthFlow performs the browser-based OAuth2 consent flow.
func runAuthFlow(credentialsFile string) error {
	config, err := loadOAuthConfig(credentialsFile)
	if err != nil {
		return err
	}

	// Start a local HTTP server to receive the callback
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("failed to start local server: %w", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	redirectURL := fmt.Sprintf("http://localhost:%d/callback", port)
	config.RedirectURL = redirectURL

	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)

	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		if code == "" {
			errMsg := r.URL.Query().Get("error")
			if errMsg == "" {
				errMsg = "no authorization code received"
			}
			fmt.Fprintf(w, "<html><body><h2>Authentication failed</h2><p>%s</p></body></html>", errMsg)
			errCh <- fmt.Errorf("auth failed: %s", errMsg)
			return
		}
		fmt.Fprintf(w, "<html><body><h2>Authentication successful!</h2><p>You can close this tab.</p></body></html>")
		codeCh <- code
	})

	server := &http.Server{Handler: mux}
	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	authURL := config.AuthCodeURL("state-token", oauth2.AccessTypeOffline, oauth2.ApprovalForce)
	fmt.Fprintf(os.Stderr, "Opening browser for authentication...\n")
	fmt.Fprintf(os.Stderr, "If the browser doesn't open, visit this URL:\n%s\n", authURL)
	openBrowser(authURL)

	var code string
	select {
	case code = <-codeCh:
	case err := <-errCh:
		server.Close()
		return err
	}

	server.Close()

	tok, err := config.Exchange(context.Background(), code)
	if err != nil {
		return fmt.Errorf("unable to exchange auth code for token: %w", err)
	}

	if err := saveToken(tok); err != nil {
		return fmt.Errorf("unable to save token: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Authentication successful! Token saved to %s\n", tokenPath())
	// Output JSON success for programmatic consumption
	result := map[string]string{"status": "authenticated", "token_path": tokenPath()}
	enc := json.NewEncoder(os.Stdout)
	return enc.Encode(result)
}

// openBrowser attempts to open a URL in the default browser.
func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}
