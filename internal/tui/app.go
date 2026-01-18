package tui

import (
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/takeshy/mcp-gatekeeper/internal/db"
)

// Screen represents different screens in the TUI
type Screen int

const (
	ScreenMain Screen = iota
	ScreenAPIKeys
	ScreenAPIKeyCreate
	ScreenAPIKeyDetail
	ScreenPolicyEdit
	ScreenAuditLogs
	ScreenAuditLogDetail
	ScreenTestExecute
)

// Styles
var (
	titleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("205")).
			Bold(true).
			Padding(0, 1)

	selectedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("170")).
			Bold(true)

	normalStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("252"))

	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("196"))

	successStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("46"))

	helpStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241"))

	boxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("62")).
			Padding(1, 2)
)

// KeyMap defines the keybindings
type KeyMap struct {
	Up     key.Binding
	Down   key.Binding
	Enter  key.Binding
	Back   key.Binding
	Quit   key.Binding
	New    key.Binding
	Delete key.Binding
	Edit   key.Binding
	Test   key.Binding
	Tab    key.Binding
}

var keys = KeyMap{
	Up: key.NewBinding(
		key.WithKeys("up", "k"),
		key.WithHelp("↑/k", "up"),
	),
	Down: key.NewBinding(
		key.WithKeys("down", "j"),
		key.WithHelp("↓/j", "down"),
	),
	Enter: key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "select"),
	),
	Back: key.NewBinding(
		key.WithKeys("esc"),
		key.WithHelp("esc", "back"),
	),
	Quit: key.NewBinding(
		key.WithKeys("q", "ctrl+c"),
		key.WithHelp("q", "quit"),
	),
	New: key.NewBinding(
		key.WithKeys("n"),
		key.WithHelp("n", "new"),
	),
	Delete: key.NewBinding(
		key.WithKeys("d"),
		key.WithHelp("d", "delete"),
	),
	Edit: key.NewBinding(
		key.WithKeys("e"),
		key.WithHelp("e", "edit"),
	),
	Test: key.NewBinding(
		key.WithKeys("t"),
		key.WithHelp("t", "test"),
	),
	Tab: key.NewBinding(
		key.WithKeys("tab"),
		key.WithHelp("tab", "next field"),
	),
}

// MenuItem represents a menu item
type MenuItem struct {
	title       string
	description string
	screen      Screen
}

func (i MenuItem) Title() string       { return i.title }
func (i MenuItem) Description() string { return i.description }
func (i MenuItem) FilterValue() string { return i.title }

// App represents the main TUI application
type App struct {
	db           *db.DB
	screen       Screen
	screenStack  []Screen
	width        int
	height       int
	mainList     list.Model
	err          error
	message      string

	// Sub-models
	apiKeyList       *APIKeyListModel
	apiKeyCreate     *APIKeyCreateModel
	apiKeyDetail     *APIKeyDetailModel
	policyEdit       *PolicyEditModel
	auditLogList     *AuditLogListModel
	auditLogDetail   *AuditLogDetailModel
	testExecute      *TestExecuteModel
}

// NewApp creates a new TUI application
func NewApp(database *db.DB) *App {
	items := []list.Item{
		MenuItem{title: "API Keys", description: "Manage API keys", screen: ScreenAPIKeys},
		MenuItem{title: "Audit Logs", description: "View audit logs", screen: ScreenAuditLogs},
		MenuItem{title: "Test Execute", description: "Test command execution", screen: ScreenTestExecute},
	}

	l := list.New(items, list.NewDefaultDelegate(), 0, 0)
	l.Title = "MCP Gatekeeper Admin"
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(false)

	return &App{
		db:       database,
		screen:   ScreenMain,
		mainList: l,
	}
}

// Init implements tea.Model
func (a *App) Init() tea.Cmd {
	return nil
}

// Update implements tea.Model
func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height
		a.mainList.SetSize(msg.Width-4, msg.Height-6)
		return a, nil

	case tea.KeyMsg:
		// Global quit handling
		if key.Matches(msg, keys.Quit) && a.screen == ScreenMain {
			return a, tea.Quit
		}
	}

	// Delegate to screen-specific handlers
	switch a.screen {
	case ScreenMain:
		return a.updateMain(msg)
	case ScreenAPIKeys:
		return a.updateAPIKeys(msg)
	case ScreenAPIKeyCreate:
		return a.updateAPIKeyCreate(msg)
	case ScreenAPIKeyDetail:
		return a.updateAPIKeyDetail(msg)
	case ScreenPolicyEdit:
		return a.updatePolicyEdit(msg)
	case ScreenAuditLogs:
		return a.updateAuditLogs(msg)
	case ScreenAuditLogDetail:
		return a.updateAuditLogDetail(msg)
	case ScreenTestExecute:
		return a.updateTestExecute(msg)
	}

	return a, nil
}

// View implements tea.Model
func (a *App) View() string {
	switch a.screen {
	case ScreenMain:
		return a.viewMain()
	case ScreenAPIKeys:
		return a.viewAPIKeys()
	case ScreenAPIKeyCreate:
		return a.viewAPIKeyCreate()
	case ScreenAPIKeyDetail:
		return a.viewAPIKeyDetail()
	case ScreenPolicyEdit:
		return a.viewPolicyEdit()
	case ScreenAuditLogs:
		return a.viewAuditLogs()
	case ScreenAuditLogDetail:
		return a.viewAuditLogDetail()
	case ScreenTestExecute:
		return a.viewTestExecute()
	}
	return ""
}

func (a *App) pushScreen(screen Screen) {
	a.screenStack = append(a.screenStack, a.screen)
	a.screen = screen
	a.message = ""
	a.err = nil
}

func (a *App) popScreen() {
	if len(a.screenStack) > 0 {
		a.screen = a.screenStack[len(a.screenStack)-1]
		a.screenStack = a.screenStack[:len(a.screenStack)-1]
	}
	a.message = ""
	a.err = nil
}

func (a *App) updateMain(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if key.Matches(msg, keys.Enter) {
			if item, ok := a.mainList.SelectedItem().(MenuItem); ok {
				a.pushScreen(item.screen)
				return a, a.initScreen(item.screen)
			}
		}
	}

	var cmd tea.Cmd
	a.mainList, cmd = a.mainList.Update(msg)
	return a, cmd
}

func (a *App) initScreen(screen Screen) tea.Cmd {
	switch screen {
	case ScreenAPIKeys:
		a.apiKeyList = NewAPIKeyListModel(a.db, a.width, a.height)
		return a.apiKeyList.Init()
	case ScreenAuditLogs:
		a.auditLogList = NewAuditLogListModel(a.db, a.width, a.height)
		return a.auditLogList.Init()
	case ScreenTestExecute:
		a.testExecute = NewTestExecuteModel(a.db, a.width, a.height)
		return a.testExecute.Init()
	}
	return nil
}

func (a *App) viewMain() string {
	return lipgloss.JoinVertical(
		lipgloss.Left,
		a.mainList.View(),
		helpStyle.Render("\n  q: quit • enter: select"),
	)
}
