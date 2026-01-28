package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/takeshy/mcp-gatekeeper/internal/db"
	"github.com/takeshy/mcp-gatekeeper/internal/version"
)

// Screen represents the current screen
type Screen int

const (
	ScreenMain Screen = iota
	ScreenOAuthClients
	ScreenOAuthClientNew
	ScreenOAuthClientCreated
	ScreenOAuthClientConfirmDelete
	ScreenAuditLogs
)

// Styles
var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#7C3AED")).
			MarginBottom(1)

	menuItemStyle = lipgloss.NewStyle().
			PaddingLeft(2)

	selectedItemStyle = lipgloss.NewStyle().
				PaddingLeft(2).
				Foreground(lipgloss.Color("#7C3AED")).
				Bold(true)

	helpStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#666666")).
			MarginTop(1)

	successStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#10B981"))

	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#EF4444"))

	warningStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#F59E0B"))

	boxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#7C3AED")).
			Padding(1, 2)
)

// App represents the TUI application
type App struct {
	db            *db.DB
	screen        Screen
	cursor        int
	width         int
	height        int
	err           error
	message       string
	oauthClients  []*db.OAuthClient
	selectedClient *db.OAuthClient
	newClientID   string
	newClientSecret string
	inputMode     bool
	inputValue    string
}

// NewApp creates a new TUI application
func NewApp(database *db.DB) *App {
	return &App{
		db:     database,
		screen: ScreenMain,
	}
}

// Init initializes the app
func (a *App) Init() tea.Cmd {
	return nil
}

// Update handles messages
func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return a.handleKeyPress(msg)
	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height
	}
	return a, nil
}

func (a *App) handleKeyPress(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Handle input mode
	if a.inputMode {
		return a.handleInputMode(msg)
	}

	switch msg.String() {
	case "q", "ctrl+c":
		if a.screen == ScreenMain {
			return a, tea.Quit
		}
		// Go back to main screen
		a.screen = ScreenMain
		a.cursor = 0
		a.err = nil
		a.message = ""
	case "up", "k":
		if a.cursor > 0 {
			a.cursor--
		}
	case "down", "j":
		a.cursor++
	case "enter":
		return a.handleEnter()
	case "esc":
		if a.screen != ScreenMain {
			a.screen = ScreenMain
			a.cursor = 0
			a.err = nil
			a.message = ""
		}
	case "d":
		// Delete client
		if a.screen == ScreenOAuthClients && len(a.oauthClients) > 0 && a.cursor < len(a.oauthClients) {
			a.selectedClient = a.oauthClients[a.cursor]
			a.screen = ScreenOAuthClientConfirmDelete
			a.cursor = 0
		}
	case "r":
		// Revoke client
		if a.screen == ScreenOAuthClients && len(a.oauthClients) > 0 && a.cursor < len(a.oauthClients) {
			client := a.oauthClients[a.cursor]
			if client.Status == "active" {
				if err := a.db.RevokeOAuthClient(client.ID); err != nil {
					a.err = err
				} else {
					a.message = fmt.Sprintf("Client '%s' revoked", client.ClientID)
					a.loadOAuthClients()
				}
			}
		}
	}
	return a, nil
}

func (a *App) handleInputMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		if a.screen == ScreenOAuthClientNew {
			// Create new client
			clientID := strings.TrimSpace(a.inputValue)
			if clientID == "" {
				a.err = fmt.Errorf("client ID cannot be empty")
				return a, nil
			}
			clientSecret, err := a.db.CreateOAuthClient(clientID)
			if err != nil {
				a.err = err
				return a, nil
			}
			a.newClientID = clientID
			a.newClientSecret = clientSecret
			a.inputMode = false
			a.inputValue = ""
			a.screen = ScreenOAuthClientCreated
		}
	case "esc":
		a.inputMode = false
		a.inputValue = ""
		a.screen = ScreenOAuthClients
		a.cursor = 0
	case "backspace":
		if len(a.inputValue) > 0 {
			a.inputValue = a.inputValue[:len(a.inputValue)-1]
		}
	default:
		if len(msg.String()) == 1 {
			a.inputValue += msg.String()
		}
	}
	return a, nil
}

func (a *App) handleEnter() (tea.Model, tea.Cmd) {
	switch a.screen {
	case ScreenMain:
		return a.handleMainMenuEnter()
	case ScreenOAuthClients:
		// Handle menu at bottom of client list
		menuOffset := len(a.oauthClients)
		if a.cursor == menuOffset {
			// New Client
			a.screen = ScreenOAuthClientNew
			a.inputMode = true
			a.inputValue = ""
			a.err = nil
			a.message = ""
		} else if a.cursor == menuOffset+1 {
			// Back
			a.screen = ScreenMain
			a.cursor = 0
		}
	case ScreenOAuthClientCreated:
		a.screen = ScreenOAuthClients
		a.cursor = 0
		a.loadOAuthClients()
	case ScreenOAuthClientConfirmDelete:
		if a.cursor == 0 {
			// Confirm delete
			if err := a.db.DeleteOAuthClient(a.selectedClient.ID); err != nil {
				a.err = err
			} else {
				a.message = fmt.Sprintf("Client '%s' deleted", a.selectedClient.ClientID)
			}
			a.selectedClient = nil
			a.screen = ScreenOAuthClients
			a.cursor = 0
			a.loadOAuthClients()
		} else {
			// Cancel
			a.selectedClient = nil
			a.screen = ScreenOAuthClients
			a.cursor = 0
		}
	}
	return a, nil
}

func (a *App) handleMainMenuEnter() (tea.Model, tea.Cmd) {
	switch a.cursor {
	case 0: // OAuth Clients
		a.screen = ScreenOAuthClients
		a.cursor = 0
		a.err = nil
		a.message = ""
		a.loadOAuthClients()
	case 1: // Audit Logs
		a.screen = ScreenAuditLogs
		a.cursor = 0
	case 2: // Quit
		return a, tea.Quit
	}
	return a, nil
}

func (a *App) loadOAuthClients() {
	clients, err := a.db.ListOAuthClients()
	if err != nil {
		a.err = err
		return
	}
	a.oauthClients = clients
}

// View renders the UI
func (a *App) View() string {
	var b strings.Builder

	switch a.screen {
	case ScreenMain:
		b.WriteString(a.viewMain())
	case ScreenOAuthClients:
		b.WriteString(a.viewOAuthClients())
	case ScreenOAuthClientNew:
		b.WriteString(a.viewOAuthClientNew())
	case ScreenOAuthClientCreated:
		b.WriteString(a.viewOAuthClientCreated())
	case ScreenOAuthClientConfirmDelete:
		b.WriteString(a.viewOAuthClientConfirmDelete())
	case ScreenAuditLogs:
		b.WriteString(a.viewAuditLogs())
	}

	return b.String()
}

func (a *App) viewMain() string {
	var b strings.Builder

	b.WriteString(titleStyle.Render("MCP Gatekeeper Admin"))
	b.WriteString("\n")
	b.WriteString(fmt.Sprintf("Version: %s\n\n", version.Version))

	menuItems := []string{
		"OAuth Clients",
		"Audit Logs",
		"Quit",
	}

	// Clamp cursor
	if a.cursor >= len(menuItems) {
		a.cursor = len(menuItems) - 1
	}

	for i, item := range menuItems {
		if i == a.cursor {
			b.WriteString(selectedItemStyle.Render("> " + item))
		} else {
			b.WriteString(menuItemStyle.Render("  " + item))
		}
		b.WriteString("\n")
	}

	b.WriteString(helpStyle.Render("\n[j/k] Navigate  [Enter] Select  [q] Quit"))

	return b.String()
}

func (a *App) viewOAuthClients() string {
	var b strings.Builder

	b.WriteString(titleStyle.Render("OAuth Clients"))
	b.WriteString("\n\n")

	if a.err != nil {
		b.WriteString(errorStyle.Render(fmt.Sprintf("Error: %v", a.err)))
		b.WriteString("\n\n")
	}

	if a.message != "" {
		b.WriteString(successStyle.Render(a.message))
		b.WriteString("\n\n")
	}

	if len(a.oauthClients) == 0 {
		b.WriteString("No OAuth clients found.\n\n")
	} else {
		for i, client := range a.oauthClients {
			statusStr := successStyle.Render("active")
			if client.Status == "revoked" {
				statusStr = errorStyle.Render("revoked")
			}

			item := fmt.Sprintf("%s [%s] - Created: %s",
				client.ClientID,
				statusStr,
				client.CreatedAt.Format("2006-01-02 15:04"))

			if i == a.cursor {
				b.WriteString(selectedItemStyle.Render("> " + item))
			} else {
				b.WriteString(menuItemStyle.Render("  " + item))
			}
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	// Menu options
	menuItems := []string{"[New Client]", "[Back]"}
	menuOffset := len(a.oauthClients)

	for i, item := range menuItems {
		if a.cursor == menuOffset+i {
			b.WriteString(selectedItemStyle.Render("> " + item))
		} else {
			b.WriteString(menuItemStyle.Render("  " + item))
		}
		b.WriteString("\n")
	}

	// Clamp cursor
	maxCursor := menuOffset + len(menuItems) - 1
	if a.cursor > maxCursor {
		a.cursor = maxCursor
	}

	b.WriteString(helpStyle.Render("\n[j/k] Navigate  [Enter] Select  [r] Revoke  [d] Delete  [Esc] Back"))

	return b.String()
}

func (a *App) viewOAuthClientNew() string {
	var b strings.Builder

	b.WriteString(titleStyle.Render("New OAuth Client"))
	b.WriteString("\n\n")

	if a.err != nil {
		b.WriteString(errorStyle.Render(fmt.Sprintf("Error: %v", a.err)))
		b.WriteString("\n\n")
	}

	b.WriteString("Enter Client ID:\n\n")
	b.WriteString(boxStyle.Render(a.inputValue + "_"))
	b.WriteString("\n")

	b.WriteString(helpStyle.Render("\n[Enter] Create  [Esc] Cancel"))

	return b.String()
}

func (a *App) viewOAuthClientCreated() string {
	var b strings.Builder

	b.WriteString(titleStyle.Render("OAuth Client Created"))
	b.WriteString("\n\n")

	b.WriteString(successStyle.Render("Client created successfully!"))
	b.WriteString("\n\n")

	b.WriteString(warningStyle.Render("IMPORTANT: Save the client secret now. It will not be shown again!"))
	b.WriteString("\n\n")

	content := fmt.Sprintf("Client ID:     %s\nClient Secret: %s", a.newClientID, a.newClientSecret)
	b.WriteString(boxStyle.Render(content))
	b.WriteString("\n")

	b.WriteString(helpStyle.Render("\n[Enter] Continue"))

	return b.String()
}

func (a *App) viewOAuthClientConfirmDelete() string {
	var b strings.Builder

	b.WriteString(titleStyle.Render("Delete OAuth Client"))
	b.WriteString("\n\n")

	b.WriteString(warningStyle.Render(fmt.Sprintf("Are you sure you want to delete client '%s'?", a.selectedClient.ClientID)))
	b.WriteString("\n")
	b.WriteString(warningStyle.Render("This action cannot be undone. All tokens will be deleted."))
	b.WriteString("\n\n")

	menuItems := []string{"Yes, delete", "No, cancel"}
	for i, item := range menuItems {
		if i == a.cursor {
			b.WriteString(selectedItemStyle.Render("> " + item))
		} else {
			b.WriteString(menuItemStyle.Render("  " + item))
		}
		b.WriteString("\n")
	}

	// Clamp cursor
	if a.cursor > 1 {
		a.cursor = 1
	}

	b.WriteString(helpStyle.Render("\n[j/k] Navigate  [Enter] Select"))

	return b.String()
}

func (a *App) viewAuditLogs() string {
	var b strings.Builder

	b.WriteString(titleStyle.Render("Audit Logs"))
	b.WriteString("\n\n")

	stats, err := a.db.GetAuditStats()
	if err != nil {
		b.WriteString(errorStyle.Render(fmt.Sprintf("Error: %v", err)))
		b.WriteString("\n")
	} else {
		b.WriteString("Log counts by mode:\n\n")
		for mode, count := range stats {
			b.WriteString(fmt.Sprintf("  %s: %d\n", mode, count))
		}
		if len(stats) == 0 {
			b.WriteString("  (no audit logs)\n")
		}
	}

	b.WriteString(helpStyle.Render("\n[Esc/q] Back"))

	return b.String()
}
