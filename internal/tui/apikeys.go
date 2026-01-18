package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/takeshy/mcp-gatekeeper/internal/auth"
	"github.com/takeshy/mcp-gatekeeper/internal/db"
	"github.com/takeshy/mcp-gatekeeper/internal/policy"
)

// APIKeyItem represents an API key in the list
type APIKeyItem struct {
	key *db.APIKey
}

func (i APIKeyItem) Title() string {
	status := "●"
	if i.key.Status == "revoked" {
		status = "○"
	}
	return fmt.Sprintf("%s %s", status, i.key.Name)
}
func (i APIKeyItem) Description() string {
	return fmt.Sprintf("ID: %d | Created: %s", i.key.ID, i.key.CreatedAt.Format("2006-01-02 15:04"))
}
func (i APIKeyItem) FilterValue() string { return i.key.Name }

// APIKeyListModel handles the API key list view
type APIKeyListModel struct {
	db     *db.DB
	list   list.Model
	width  int
	height int
	err    error
}

// NewAPIKeyListModel creates a new API key list model
func NewAPIKeyListModel(database *db.DB, width, height int) *APIKeyListModel {
	l := list.New(nil, list.NewDefaultDelegate(), width-4, height-8)
	l.Title = "API Keys"
	l.SetShowStatusBar(true)
	l.SetFilteringEnabled(true)

	return &APIKeyListModel{
		db:     database,
		list:   l,
		width:  width,
		height: height,
	}
}

// Init initializes the model
func (m *APIKeyListModel) Init() tea.Cmd {
	return m.loadAPIKeys
}

func (m *APIKeyListModel) loadAPIKeys() tea.Msg {
	keys, err := m.db.ListAPIKeys()
	if err != nil {
		return errMsg{err}
	}
	return apiKeysMsg{keys}
}

type apiKeysMsg struct {
	keys []*db.APIKey
}

type errMsg struct {
	err error
}

// Update handles messages
func (m *APIKeyListModel) Update(msg tea.Msg) (*APIKeyListModel, tea.Cmd) {
	switch msg := msg.(type) {
	case apiKeysMsg:
		items := make([]list.Item, len(msg.keys))
		for i, k := range msg.keys {
			items[i] = APIKeyItem{key: k}
		}
		m.list.SetItems(items)
		return m, nil

	case errMsg:
		m.err = msg.err
		return m, nil
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

// View renders the model
func (m *APIKeyListModel) View() string {
	if m.err != nil {
		return errorStyle.Render(fmt.Sprintf("Error: %v", m.err))
	}
	return lipgloss.JoinVertical(
		lipgloss.Left,
		m.list.View(),
		helpStyle.Render("\n  n: new • d: revoke • t: tools • v: env vars • enter: details • esc: back"),
	)
}

// SelectedKey returns the selected API key
func (m *APIKeyListModel) SelectedKey() *db.APIKey {
	if item, ok := m.list.SelectedItem().(APIKeyItem); ok {
		return item.key
	}
	return nil
}

// APIKeyCreateModel handles API key creation
type APIKeyCreateModel struct {
	db        *db.DB
	nameInput textinput.Model
	focused   bool
	err       error
	created   bool
	apiKey    string
	keyRecord *db.APIKey
}

// NewAPIKeyCreateModel creates a new API key creation model
func NewAPIKeyCreateModel(database *db.DB) *APIKeyCreateModel {
	ti := textinput.New()
	ti.Placeholder = "Enter key name"
	ti.Focus()
	ti.CharLimit = 100
	ti.Width = 40

	return &APIKeyCreateModel{
		db:        database,
		nameInput: ti,
		focused:   true,
	}
}

// Init initializes the model
func (m *APIKeyCreateModel) Init() tea.Cmd {
	return textinput.Blink
}

// Update handles messages
func (m *APIKeyCreateModel) Update(msg tea.Msg) (*APIKeyCreateModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "enter":
			if m.nameInput.Value() != "" && !m.created {
				authenticator := auth.NewAuthenticator(m.db)
				apiKey, keyRecord, err := authenticator.CreateAPIKey(m.nameInput.Value())
				if err != nil {
					m.err = err
					return m, nil
				}

				m.created = true
				m.apiKey = apiKey
				m.keyRecord = keyRecord
				return m, nil
			}
		}
	}

	var cmd tea.Cmd
	m.nameInput, cmd = m.nameInput.Update(msg)
	return m, cmd
}

// View renders the model
func (m *APIKeyCreateModel) View() string {
	var sb strings.Builder

	sb.WriteString(titleStyle.Render("Create New API Key"))
	sb.WriteString("\n\n")

	if m.created {
		sb.WriteString(successStyle.Render("API Key Created Successfully!"))
		sb.WriteString("\n\n")
		sb.WriteString(fmt.Sprintf("Name: %s\n", m.keyRecord.Name))
		sb.WriteString(fmt.Sprintf("ID: %d\n\n", m.keyRecord.ID))
		sb.WriteString(boxStyle.Render(fmt.Sprintf("API Key (save this, it won't be shown again):\n\n%s", m.apiKey)))
		sb.WriteString("\n\n")
		sb.WriteString(helpStyle.Render("Press esc to go back. Then add tools with 't' key."))
	} else {
		sb.WriteString("Key Name:\n")
		sb.WriteString(m.nameInput.View())
		sb.WriteString("\n\n")

		if m.err != nil {
			sb.WriteString(errorStyle.Render(fmt.Sprintf("Error: %v", m.err)))
			sb.WriteString("\n\n")
		}

		sb.WriteString(helpStyle.Render("enter: create • esc: cancel"))
	}

	return sb.String()
}

// APIKeyDetailModel handles API key detail view
type APIKeyDetailModel struct {
	db         *db.DB
	apiKey     *db.APIKey
	toolsCount int
	err        error
}

// NewAPIKeyDetailModel creates a new API key detail model
func NewAPIKeyDetailModel(database *db.DB, apiKey *db.APIKey) *APIKeyDetailModel {
	return &APIKeyDetailModel{
		db:     database,
		apiKey: apiKey,
	}
}

// Init initializes the model
func (m *APIKeyDetailModel) Init() tea.Cmd {
	return m.loadToolsCount
}

func (m *APIKeyDetailModel) loadToolsCount() tea.Msg {
	tools, err := m.db.ListToolsByAPIKeyID(m.apiKey.ID)
	if err != nil {
		return errMsg{err}
	}
	return toolsCountMsg{len(tools)}
}

type toolsCountMsg struct {
	count int
}

// Update handles messages
func (m *APIKeyDetailModel) Update(msg tea.Msg) (*APIKeyDetailModel, tea.Cmd) {
	switch msg := msg.(type) {
	case toolsCountMsg:
		m.toolsCount = msg.count
		return m, nil
	case errMsg:
		m.err = msg.err
		return m, nil
	}
	return m, nil
}

// View renders the model
func (m *APIKeyDetailModel) View() string {
	var sb strings.Builder

	sb.WriteString(titleStyle.Render("API Key Details"))
	sb.WriteString("\n\n")

	sb.WriteString(fmt.Sprintf("Name:       %s\n", m.apiKey.Name))
	sb.WriteString(fmt.Sprintf("ID:         %d\n", m.apiKey.ID))
	sb.WriteString(fmt.Sprintf("Status:     %s\n", m.apiKey.Status))
	sb.WriteString(fmt.Sprintf("Created:    %s\n", m.apiKey.CreatedAt.Format("2006-01-02 15:04:05")))
	if m.apiKey.LastUsedAt.Valid {
		sb.WriteString(fmt.Sprintf("Last Used:  %s\n", m.apiKey.LastUsedAt.Time.Format("2006-01-02 15:04:05")))
	}
	if m.apiKey.RevokedAt.Valid {
		sb.WriteString(fmt.Sprintf("Revoked At: %s\n", m.apiKey.RevokedAt.Time.Format("2006-01-02 15:04:05")))
	}

	sb.WriteString("\n")
	sb.WriteString(titleStyle.Render("Configuration"))
	sb.WriteString("\n\n")
	sb.WriteString(fmt.Sprintf("Tools:            %d configured\n", m.toolsCount))
	if len(m.apiKey.AllowedEnvKeys) == 0 {
		sb.WriteString("Allowed Env Keys: (no restrictions - all env vars allowed)\n")
	} else {
		sb.WriteString(fmt.Sprintf("Allowed Env Keys: %v\n", m.apiKey.AllowedEnvKeys))
	}

	sb.WriteString("\n")
	if m.err != nil {
		sb.WriteString(errorStyle.Render(fmt.Sprintf("Error: %v", m.err)))
		sb.WriteString("\n")
	}

	sb.WriteString(helpStyle.Render("\nt: manage tools • v: edit env vars • r: revoke • esc: back"))

	return sb.String()
}

// EnvKeysEditModel handles editing allowed environment keys
type EnvKeysEditModel struct {
	db             *db.DB
	apiKey         *db.APIKey
	input          textarea.Model
	err            error
	saved          bool
	showCompletion bool
}

// NewEnvKeysEditModel creates a new env keys edit model
func NewEnvKeysEditModel(database *db.DB, apiKey *db.APIKey) *EnvKeysEditModel {
	ta := textarea.New()
	ta.Placeholder = "Enter allowed env key patterns, one per line (e.g., PATH, HOME, USER)"
	ta.SetValue(strings.Join(apiKey.AllowedEnvKeys, "\n"))
	ta.CharLimit = 0
	ta.SetWidth(60)
	ta.SetHeight(10)
	ta.Focus()

	return &EnvKeysEditModel{
		db:     database,
		apiKey: apiKey,
		input:  ta,
	}
}

// Init initializes the model
func (m *EnvKeysEditModel) Init() tea.Cmd {
	return textarea.Blink
}

// Update handles messages
func (m *EnvKeysEditModel) Update(msg tea.Msg) (*EnvKeysEditModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+s":
			return m, m.save
		}
	case envKeysSavedMsg:
		m.saved = true
		return m, nil
	case errMsg:
		m.err = msg.err
		return m, nil
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m *EnvKeysEditModel) save() tea.Msg {
	keys := parseLines(m.input.Value())

	// Validate patterns
	if err := policy.ValidateAllowedEnvKeys(keys); err != nil {
		return errMsg{err}
	}

	// Save
	if err := m.db.UpdateAPIKeyAllowedEnvKeys(m.apiKey.ID, keys); err != nil {
		return errMsg{err}
	}

	// Update local copy
	m.apiKey.AllowedEnvKeys = keys

	return envKeysSavedMsg{}
}

type envKeysSavedMsg struct{}

// View renders the model
func (m *EnvKeysEditModel) View() string {
	var sb strings.Builder

	sb.WriteString(titleStyle.Render("Edit Allowed Environment Keys"))
	sb.WriteString("\n\n")

	if m.saved {
		sb.WriteString(successStyle.Render("Environment Keys Updated Successfully!"))
		sb.WriteString("\n\n")
		sb.WriteString(helpStyle.Render("Press esc to go back"))
	} else {
		sb.WriteString("Allowed patterns (one per line):\n")
		sb.WriteString("Empty = all env vars allowed\n\n")
		sb.WriteString(m.input.View())
		sb.WriteString("\n\n")

		if m.err != nil {
			sb.WriteString(errorStyle.Render(fmt.Sprintf("Error: %v", m.err)))
			sb.WriteString("\n\n")
		}

		sb.WriteString(helpStyle.Render("ctrl+s: save • esc: cancel"))
	}

	return sb.String()
}

// App screen handlers for API keys
func (a *App) updateAPIKeys(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if key.Matches(msg, keys.Back) {
			a.popScreen()
			return a, nil
		}
		if key.Matches(msg, keys.New) {
			a.apiKeyCreate = NewAPIKeyCreateModel(a.db)
			a.pushScreen(ScreenAPIKeyCreate)
			return a, a.apiKeyCreate.Init()
		}
		if key.Matches(msg, keys.Enter) {
			if selected := a.apiKeyList.SelectedKey(); selected != nil {
				a.apiKeyDetail = NewAPIKeyDetailModel(a.db, selected)
				a.pushScreen(ScreenAPIKeyDetail)
				return a, a.apiKeyDetail.Init()
			}
		}
		if key.Matches(msg, keys.Delete) {
			if selected := a.apiKeyList.SelectedKey(); selected != nil {
				if err := a.db.RevokeAPIKey(selected.ID); err != nil {
					a.err = err
				} else {
					a.message = "API key revoked"
				}
				return a, a.apiKeyList.loadAPIKeys
			}
		}
		if key.Matches(msg, keys.Test) { // 't' for tools
			if selected := a.apiKeyList.SelectedKey(); selected != nil {
				a.selectedAPIKey = selected
				a.toolList = NewToolListModel(a.db, selected.ID, a.width, a.height)
				a.pushScreen(ScreenToolList)
				return a, a.toolList.Init()
			}
		}
		if msg.String() == "v" { // 'v' for env vars
			if selected := a.apiKeyList.SelectedKey(); selected != nil {
				a.envKeysEdit = NewEnvKeysEditModel(a.db, selected)
				a.pushScreen(ScreenEnvKeysEdit)
				return a, a.envKeysEdit.Init()
			}
		}
	}

	var cmd tea.Cmd
	a.apiKeyList, cmd = a.apiKeyList.Update(msg)
	return a, cmd
}

func (a *App) updateAPIKeyCreate(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if key.Matches(msg, keys.Back) {
			a.popScreen()
			return a, a.apiKeyList.loadAPIKeys
		}
	}

	var cmd tea.Cmd
	a.apiKeyCreate, cmd = a.apiKeyCreate.Update(msg)
	return a, cmd
}

func (a *App) updateAPIKeyDetail(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if key.Matches(msg, keys.Back) {
			a.popScreen()
			return a, nil
		}
		if key.Matches(msg, keys.Test) { // 't' for tools
			a.selectedAPIKey = a.apiKeyDetail.apiKey
			a.toolList = NewToolListModel(a.db, a.apiKeyDetail.apiKey.ID, a.width, a.height)
			a.pushScreen(ScreenToolList)
			return a, a.toolList.Init()
		}
		if msg.String() == "v" { // 'v' for env vars
			a.envKeysEdit = NewEnvKeysEditModel(a.db, a.apiKeyDetail.apiKey)
			a.pushScreen(ScreenEnvKeysEdit)
			return a, a.envKeysEdit.Init()
		}
		if msg.String() == "r" { // 'r' for revoke
			if err := a.db.RevokeAPIKey(a.apiKeyDetail.apiKey.ID); err != nil {
				a.err = err
			} else {
				a.message = "API key revoked"
				a.popScreen()
				return a, a.apiKeyList.loadAPIKeys
			}
		}
	}

	var cmd tea.Cmd
	a.apiKeyDetail, cmd = a.apiKeyDetail.Update(msg)
	return a, cmd
}

func (a *App) viewAPIKeys() string {
	if a.apiKeyList == nil {
		return "Loading..."
	}
	view := a.apiKeyList.View()
	if a.message != "" {
		view += "\n" + successStyle.Render(a.message)
	}
	if a.err != nil {
		view += "\n" + errorStyle.Render(fmt.Sprintf("Error: %v", a.err))
	}
	return view
}

func (a *App) viewAPIKeyCreate() string {
	if a.apiKeyCreate == nil {
		return "Loading..."
	}
	return a.apiKeyCreate.View()
}

func (a *App) viewAPIKeyDetail() string {
	if a.apiKeyDetail == nil {
		return "Loading..."
	}
	return a.apiKeyDetail.View()
}

func parseLines(s string) []string {
	var result []string
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			result = append(result, line)
		}
	}
	return result
}
