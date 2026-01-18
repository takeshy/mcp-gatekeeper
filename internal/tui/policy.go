package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/takeshy/mcp-gatekeeper/internal/db"
	"github.com/takeshy/mcp-gatekeeper/internal/policy"
)

// CompletionItem represents a path completion item
type CompletionItem struct {
	path string
}

func (i CompletionItem) Title() string       { return i.path }
func (i CompletionItem) Description() string { return "" }
func (i CompletionItem) FilterValue() string { return i.path }

// PolicyEditModel handles policy editing
type PolicyEditModel struct {
	db             *db.DB
	policy         *db.Policy
	focusedField   int
	inputs         []textarea.Model
	fieldNames     []string
	precedenceIdx  int
	err            error
	saved          bool
	pathCompleter  *PathCompleter
	showCompletion bool
	completionList list.Model
	completions    []string
}

// NewPolicyEditModel creates a new policy edit model
func NewPolicyEditModel(database *db.DB, pol *db.Policy) *PolicyEditModel {
	fieldNames := []string{
		"Allowed CWD Globs (one per line) [Ctrl+Space: path completion]",
		"Allowed Cmd Globs (one per line)",
		"Denied Cmd Globs (one per line)",
		"Allowed Env Keys (one per line)",
	}

	inputs := make([]textarea.Model, 4)
	for i := range inputs {
		ta := textarea.New()
		ta.Placeholder = "Enter patterns, one per line"
		ta.CharLimit = 0
		ta.SetWidth(60)
		ta.SetHeight(5)
		inputs[i] = ta
	}

	// Set initial values
	inputs[0].SetValue(strings.Join(pol.AllowedCwdGlobs, "\n"))
	inputs[1].SetValue(strings.Join(pol.AllowedCmdGlobs, "\n"))
	inputs[2].SetValue(strings.Join(pol.DeniedCmdGlobs, "\n"))
	inputs[3].SetValue(strings.Join(pol.AllowedEnvKeys, "\n"))

	inputs[0].Focus()

	precedenceIdx := 0
	if pol.Precedence == db.PrecedenceAllowOverrides {
		precedenceIdx = 1
	}

	// Initialize completion list
	delegate := list.NewDefaultDelegate()
	delegate.ShowDescription = false
	l := list.New(nil, delegate, 60, 8)
	l.SetShowTitle(false)
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(false)
	l.SetShowHelp(false)

	return &PolicyEditModel{
		db:             database,
		policy:         pol,
		focusedField:   0,
		inputs:         inputs,
		fieldNames:     fieldNames,
		precedenceIdx:  precedenceIdx,
		pathCompleter:  NewPathCompleter(),
		completionList: l,
	}
}

// Init initializes the model
func (m *PolicyEditModel) Init() tea.Cmd {
	return textarea.Blink
}

// Update handles messages
func (m *PolicyEditModel) Update(msg tea.Msg) (*PolicyEditModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		// Handle completion mode
		if m.showCompletion {
			return m.handleCompletionMode(msg)
		}

		switch msg.String() {
		case "ctrl+s":
			return m, m.save
		case "ctrl+ ", "ctrl+space":
			// Only show completion for CWD field (index 0)
			if m.focusedField == 0 {
				m.showPathCompletion()
				return m, nil
			}
		case "tab":
			// Move focus to next field
			m.focusedField = (m.focusedField + 1) % len(m.inputs)
			for i := range m.inputs {
				if i == m.focusedField {
					m.inputs[i].Focus()
				} else {
					m.inputs[i].Blur()
				}
			}
			return m, nil
		case "shift+tab":
			// Move focus to previous field
			m.focusedField = (m.focusedField - 1 + len(m.inputs)) % len(m.inputs)
			for i := range m.inputs {
				if i == m.focusedField {
					m.inputs[i].Focus()
				} else {
					m.inputs[i].Blur()
				}
			}
			return m, nil
		case "ctrl+p":
			// Toggle precedence
			m.precedenceIdx = (m.precedenceIdx + 1) % 2
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.inputs[m.focusedField], cmd = m.inputs[m.focusedField].Update(msg)
	return m, cmd
}

func (m *PolicyEditModel) handleCompletionMode(msg tea.KeyMsg) (*PolicyEditModel, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.showCompletion = false
		return m, nil
	case "enter":
		// Apply selected completion
		if item, ok := m.completionList.SelectedItem().(CompletionItem); ok {
			m.applyCompletion(item.path)
		}
		m.showCompletion = false
		return m, nil
	case "tab":
		// Apply selected completion and continue
		if item, ok := m.completionList.SelectedItem().(CompletionItem); ok {
			m.applyCompletion(item.path)
			// Refresh completions based on new path
			m.showPathCompletion()
		}
		return m, nil
	}

	var cmd tea.Cmd
	m.completionList, cmd = m.completionList.Update(msg)
	return m, cmd
}

func (m *PolicyEditModel) showPathCompletion() {
	// Get current line from the textarea
	value := m.inputs[0].Value()
	lines := strings.Split(value, "\n")

	// Find current line (last line for simplicity, or we could track cursor)
	currentLine := ""
	if len(lines) > 0 {
		currentLine = lines[len(lines)-1]
	}

	// Get path to complete
	pathToComplete := m.pathCompleter.GetCurrentPathFromLine(currentLine)
	if pathToComplete == "" {
		pathToComplete = "/"
	}

	// Get completions
	completions := m.pathCompleter.Complete(pathToComplete)
	m.completions = completions

	// Update list
	items := make([]list.Item, len(completions))
	for i, c := range completions {
		items[i] = CompletionItem{path: c}
	}
	m.completionList.SetItems(items)

	if len(completions) > 0 {
		m.showCompletion = true
	}
}

func (m *PolicyEditModel) applyCompletion(completion string) {
	value := m.inputs[0].Value()
	lines := strings.Split(value, "\n")

	if len(lines) > 0 {
		// Replace the last line with the completion
		lastLine := lines[len(lines)-1]
		newLine := m.pathCompleter.ReplacePathInLine(lastLine, completion)
		lines[len(lines)-1] = newLine
		m.inputs[0].SetValue(strings.Join(lines, "\n"))
	} else {
		m.inputs[0].SetValue(completion)
	}
}

func (m *PolicyEditModel) save() tea.Msg {
	// Parse inputs
	m.policy.AllowedCwdGlobs = parseLines(m.inputs[0].Value())
	m.policy.AllowedCmdGlobs = parseLines(m.inputs[1].Value())
	m.policy.DeniedCmdGlobs = parseLines(m.inputs[2].Value())
	m.policy.AllowedEnvKeys = parseLines(m.inputs[3].Value())

	if m.precedenceIdx == 0 {
		m.policy.Precedence = db.PrecedenceDenyOverrides
	} else {
		m.policy.Precedence = db.PrecedenceAllowOverrides
	}

	// Validate
	if err := policy.ValidatePolicy(m.policy); err != nil {
		return errMsg{err}
	}

	// Save
	if err := m.db.UpdatePolicy(m.policy); err != nil {
		return errMsg{err}
	}

	return policySavedMsg{}
}

type policySavedMsg struct{}

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

// View renders the model
func (m *PolicyEditModel) View() string {
	var sb strings.Builder

	sb.WriteString(titleStyle.Render("Edit Policy"))
	sb.WriteString("\n\n")

	// Precedence selector
	precedences := []string{"deny_overrides", "allow_overrides"}
	sb.WriteString("Precedence: ")
	for i, p := range precedences {
		if i == m.precedenceIdx {
			sb.WriteString(selectedStyle.Render(fmt.Sprintf("[%s]", p)))
		} else {
			sb.WriteString(normalStyle.Render(fmt.Sprintf(" %s ", p)))
		}
		sb.WriteString(" ")
	}
	sb.WriteString("\n\n")

	// Input fields
	for i, input := range m.inputs {
		style := normalStyle
		if i == m.focusedField {
			style = selectedStyle
		}
		sb.WriteString(style.Render(m.fieldNames[i]))
		sb.WriteString("\n")
		sb.WriteString(input.View())

		// Show completion popup for CWD field
		if i == 0 && m.showCompletion && len(m.completions) > 0 {
			sb.WriteString("\n")
			sb.WriteString(boxStyle.Render(m.completionList.View()))
		}

		sb.WriteString("\n\n")
	}

	if m.saved {
		sb.WriteString(successStyle.Render("Policy saved successfully!"))
		sb.WriteString("\n\n")
	}

	if m.err != nil {
		sb.WriteString(errorStyle.Render(fmt.Sprintf("Error: %v", m.err)))
		sb.WriteString("\n\n")
	}

	if m.showCompletion {
		sb.WriteString(helpStyle.Render("↑/↓: select • tab: complete & continue • enter: complete • esc: cancel"))
	} else {
		sb.WriteString(helpStyle.Render("tab: next field • ctrl+space: path completion • ctrl+p: precedence • ctrl+s: save • esc: back"))
	}

	return sb.String()
}

// App screen handler for policy edit
func (a *App) updatePolicyEdit(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		// Don't allow back when in completion mode or editing
		if key.Matches(msg, keys.Back) && !a.policyEdit.showCompletion {
			a.popScreen()
			return a, nil
		}
	case policySavedMsg:
		a.policyEdit.saved = true
		return a, nil
	case errMsg:
		a.policyEdit.err = msg.err
		return a, nil
	}

	var cmd tea.Cmd
	a.policyEdit, cmd = a.policyEdit.Update(msg)
	return a, cmd
}

func (a *App) viewPolicyEdit() string {
	if a.policyEdit == nil {
		return "Loading..."
	}
	return a.policyEdit.View()
}
