package tui

import (
	"fmt"

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
	ScreenToolList
	ScreenToolCreate
	ScreenToolDetail
	ScreenToolEdit
	ScreenEnvKeysEdit
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
	toolList         *ToolListModel
	toolCreate       *ToolCreateModel
	toolDetail       *ToolDetailModel
	toolEdit         *ToolEditModel
	envKeysEdit      *EnvKeysEditModel
	auditLogList     *AuditLogListModel
	auditLogDetail   *AuditLogDetailModel
	testExecute      *TestExecuteModel
	selectedAPIKey   *db.APIKey // Currently selected API key for tool operations
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
	case ScreenToolList:
		return a.updateToolList(msg)
	case ScreenToolCreate:
		return a.updateToolCreate(msg)
	case ScreenToolDetail:
		return a.updateToolDetail(msg)
	case ScreenToolEdit:
		return a.updateToolEdit(msg)
	case ScreenEnvKeysEdit:
		return a.updateEnvKeysEdit(msg)
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
	case ScreenToolList:
		return a.viewToolList()
	case ScreenToolCreate:
		return a.viewToolCreate()
	case ScreenToolDetail:
		return a.viewToolDetail()
	case ScreenToolEdit:
		return a.viewToolEdit()
	case ScreenEnvKeysEdit:
		return a.viewEnvKeysEdit()
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

// Tool screen handlers
func (a *App) updateToolList(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if key.Matches(msg, keys.Back) {
			a.popScreen()
			return a, nil
		}
		if key.Matches(msg, keys.New) {
			a.toolCreate = NewToolCreateModel(a.db, a.selectedAPIKey.ID)
			a.pushScreen(ScreenToolCreate)
			return a, a.toolCreate.Init()
		}
		if key.Matches(msg, keys.Enter) {
			if selected := a.toolList.SelectedTool(); selected != nil {
				a.toolDetail = NewToolDetailModel(a.db, selected)
				a.pushScreen(ScreenToolDetail)
				return a, a.toolDetail.Init()
			}
		}
		if key.Matches(msg, keys.Delete) {
			if selected := a.toolList.SelectedTool(); selected != nil {
				if err := a.db.DeleteTool(selected.ID); err != nil {
					a.err = err
				} else {
					a.message = "Tool deleted"
				}
				return a, a.toolList.loadTools
			}
		}
		if key.Matches(msg, keys.Edit) {
			if selected := a.toolList.SelectedTool(); selected != nil {
				a.toolEdit = NewToolEditModel(a.db, selected)
				a.pushScreen(ScreenToolEdit)
				return a, a.toolEdit.Init()
			}
		}
	}

	var cmd tea.Cmd
	a.toolList, cmd = a.toolList.Update(msg)
	return a, cmd
}

func (a *App) updateToolCreate(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if key.Matches(msg, keys.Back) {
			a.popScreen()
			return a, a.toolList.loadTools
		}
	}

	var cmd tea.Cmd
	a.toolCreate, cmd = a.toolCreate.Update(msg)
	return a, cmd
}

func (a *App) updateToolDetail(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if key.Matches(msg, keys.Back) {
			a.popScreen()
			return a, nil
		}
		if key.Matches(msg, keys.Edit) {
			a.toolEdit = NewToolEditModel(a.db, a.toolDetail.tool)
			a.pushScreen(ScreenToolEdit)
			return a, a.toolEdit.Init()
		}
		if key.Matches(msg, keys.Delete) {
			if err := a.db.DeleteTool(a.toolDetail.tool.ID); err != nil {
				a.err = err
			} else {
				a.message = "Tool deleted"
				a.popScreen()
				return a, a.toolList.loadTools
			}
		}
	}

	var cmd tea.Cmd
	a.toolDetail, cmd = a.toolDetail.Update(msg)
	return a, cmd
}

func (a *App) updateToolEdit(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if key.Matches(msg, keys.Back) {
			a.popScreen()
			return a, a.toolList.loadTools
		}
	}

	var cmd tea.Cmd
	a.toolEdit, cmd = a.toolEdit.Update(msg)
	return a, cmd
}

func (a *App) updateEnvKeysEdit(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if key.Matches(msg, keys.Back) && !a.envKeysEdit.showCompletion {
			a.popScreen()
			return a, nil
		}
	case envKeysSavedMsg:
		a.envKeysEdit.saved = true
		return a, nil
	case errMsg:
		a.envKeysEdit.err = msg.err
		return a, nil
	}

	var cmd tea.Cmd
	a.envKeysEdit, cmd = a.envKeysEdit.Update(msg)
	return a, cmd
}

func (a *App) viewToolList() string {
	if a.toolList == nil {
		return "Loading..."
	}
	view := a.toolList.View()
	if a.message != "" {
		view += "\n" + successStyle.Render(a.message)
	}
	if a.err != nil {
		view += "\n" + errorStyle.Render(fmt.Sprintf("Error: %v", a.err))
	}
	return view
}

func (a *App) viewToolCreate() string {
	if a.toolCreate == nil {
		return "Loading..."
	}
	return a.toolCreate.View()
}

func (a *App) viewToolDetail() string {
	if a.toolDetail == nil {
		return "Loading..."
	}
	return a.toolDetail.View()
}

func (a *App) viewToolEdit() string {
	if a.toolEdit == nil {
		return "Loading..."
	}
	return a.toolEdit.View()
}

func (a *App) viewEnvKeysEdit() string {
	if a.envKeysEdit == nil {
		return "Loading..."
	}
	return a.envKeysEdit.View()
}
