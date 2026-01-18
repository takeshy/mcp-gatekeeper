package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/takeshy/mcp-gatekeeper/internal/db"
	"github.com/takeshy/mcp-gatekeeper/internal/policy"
)

// ToolItem represents a tool in the list
type ToolItem struct {
	tool *db.Tool
}

func (i ToolItem) Title() string {
	return fmt.Sprintf("%s (%s)", i.tool.Name, i.tool.Sandbox)
}
func (i ToolItem) Description() string {
	return fmt.Sprintf("Command: %s", i.tool.Command)
}
func (i ToolItem) FilterValue() string { return i.tool.Name }

// ToolListModel handles the tool list view
type ToolListModel struct {
	db       *db.DB
	apiKeyID int64
	list     list.Model
	width    int
	height   int
	err      error
}

// NewToolListModel creates a new tool list model
func NewToolListModel(database *db.DB, apiKeyID int64, width, height int) *ToolListModel {
	l := list.New(nil, list.NewDefaultDelegate(), width-4, height-8)
	l.Title = "Tools"
	l.SetShowStatusBar(true)
	l.SetFilteringEnabled(true)

	return &ToolListModel{
		db:       database,
		apiKeyID: apiKeyID,
		list:     l,
		width:    width,
		height:   height,
	}
}

// Init initializes the model
func (m *ToolListModel) Init() tea.Cmd {
	return m.loadTools
}

func (m *ToolListModel) loadTools() tea.Msg {
	tools, err := m.db.ListToolsByAPIKeyID(m.apiKeyID)
	if err != nil {
		return errMsg{err}
	}
	return toolsMsg{tools}
}

type toolsMsg struct {
	tools []*db.Tool
}

// Update handles messages
func (m *ToolListModel) Update(msg tea.Msg) (*ToolListModel, tea.Cmd) {
	switch msg := msg.(type) {
	case toolsMsg:
		items := make([]list.Item, len(msg.tools))
		for i, t := range msg.tools {
			items[i] = ToolItem{tool: t}
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
func (m *ToolListModel) View() string {
	if m.err != nil {
		return errorStyle.Render(fmt.Sprintf("Error: %v", m.err))
	}
	return lipgloss.JoinVertical(
		lipgloss.Left,
		m.list.View(),
		helpStyle.Render("\n  n: new tool • d: delete • e: edit • enter: details • esc: back"),
	)
}

// SelectedTool returns the selected tool
func (m *ToolListModel) SelectedTool() *db.Tool {
	if item, ok := m.list.SelectedItem().(ToolItem); ok {
		return item.tool
	}
	return nil
}

// ToolCreateModel handles tool creation
type ToolCreateModel struct {
	db           *db.DB
	apiKeyID     int64
	focusedField int
	nameInput    textinput.Model
	descInput    textinput.Model
	commandInput textinput.Model
	argsInput    textarea.Model
	sandboxIdx   int
	wasmInput    textinput.Model
	err          error
	created      bool
	tool         *db.Tool
}

var sandboxOptions = []string{"bubblewrap", "none", "wasm"}

// NewToolCreateModel creates a new tool creation model
func NewToolCreateModel(database *db.DB, apiKeyID int64) *ToolCreateModel {
	nameInput := textinput.New()
	nameInput.Placeholder = "Tool name (e.g., git)"
	nameInput.Focus()
	nameInput.CharLimit = 50
	nameInput.Width = 40

	descInput := textinput.New()
	descInput.Placeholder = "Description"
	descInput.CharLimit = 200
	descInput.Width = 60

	commandInput := textinput.New()
	commandInput.Placeholder = "Command to execute (e.g., /usr/bin/git)"
	commandInput.CharLimit = 200
	commandInput.Width = 60

	argsInput := textarea.New()
	argsInput.Placeholder = "Allowed argument patterns (one per line)"
	argsInput.CharLimit = 0
	argsInput.SetWidth(60)
	argsInput.SetHeight(5)

	wasmInput := textinput.New()
	wasmInput.Placeholder = "WASM binary path (only for wasm sandbox)"
	wasmInput.CharLimit = 200
	wasmInput.Width = 60

	return &ToolCreateModel{
		db:           database,
		apiKeyID:     apiKeyID,
		nameInput:    nameInput,
		descInput:    descInput,
		commandInput: commandInput,
		argsInput:    argsInput,
		wasmInput:    wasmInput,
	}
}

// Init initializes the model
func (m *ToolCreateModel) Init() tea.Cmd {
	return textinput.Blink
}

// Update handles messages
func (m *ToolCreateModel) Update(msg tea.Msg) (*ToolCreateModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "tab":
			m.focusedField = (m.focusedField + 1) % 6
			m.updateFocus()
			return m, nil
		case "shift+tab":
			m.focusedField = (m.focusedField - 1 + 6) % 6
			m.updateFocus()
			return m, nil
		case "ctrl+s":
			if !m.created {
				return m, m.save
			}
		case "ctrl+p":
			// Toggle sandbox (only when sandbox field is focused)
			if m.focusedField == 4 {
				m.sandboxIdx = (m.sandboxIdx + 1) % len(sandboxOptions)
			}
			return m, nil
		}
	case toolSavedMsg:
		m.created = true
		m.tool = msg.tool
		return m, nil
	case errMsg:
		m.err = msg.err
		return m, nil
	}

	return m.updateInputs(msg)
}

func (m *ToolCreateModel) updateFocus() {
	m.nameInput.Blur()
	m.descInput.Blur()
	m.commandInput.Blur()
	m.argsInput.Blur()
	m.wasmInput.Blur()

	switch m.focusedField {
	case 0:
		m.nameInput.Focus()
	case 1:
		m.descInput.Focus()
	case 2:
		m.commandInput.Focus()
	case 3:
		m.argsInput.Focus()
	case 5:
		m.wasmInput.Focus()
	}
}

func (m *ToolCreateModel) updateInputs(msg tea.Msg) (*ToolCreateModel, tea.Cmd) {
	var cmd tea.Cmd
	switch m.focusedField {
	case 0:
		m.nameInput, cmd = m.nameInput.Update(msg)
	case 1:
		m.descInput, cmd = m.descInput.Update(msg)
	case 2:
		m.commandInput, cmd = m.commandInput.Update(msg)
	case 3:
		m.argsInput, cmd = m.argsInput.Update(msg)
	case 5:
		m.wasmInput, cmd = m.wasmInput.Update(msg)
	}
	return m, cmd
}

func (m *ToolCreateModel) save() tea.Msg {
	name := strings.TrimSpace(m.nameInput.Value())
	if name == "" {
		return errMsg{fmt.Errorf("tool name is required")}
	}

	command := strings.TrimSpace(m.commandInput.Value())
	if command == "" {
		return errMsg{fmt.Errorf("command is required")}
	}

	sandbox := db.SandboxType(sandboxOptions[m.sandboxIdx])
	if sandbox == db.SandboxTypeWasm && strings.TrimSpace(m.wasmInput.Value()) == "" {
		return errMsg{fmt.Errorf("WASM binary path is required for wasm sandbox")}
	}

	tool := &db.Tool{
		APIKeyID:        m.apiKeyID,
		Name:            name,
		Description:     strings.TrimSpace(m.descInput.Value()),
		Command:         command,
		AllowedArgGlobs: parseLines(m.argsInput.Value()),
		Sandbox:         sandbox,
		WasmBinary:      strings.TrimSpace(m.wasmInput.Value()),
	}

	// Validate
	if err := policy.ValidateTool(tool); err != nil {
		return errMsg{err}
	}

	// Save
	created, err := m.db.CreateTool(tool)
	if err != nil {
		return errMsg{err}
	}

	return toolSavedMsg{tool: created}
}

type toolSavedMsg struct {
	tool *db.Tool
}

// View renders the model
func (m *ToolCreateModel) View() string {
	var sb strings.Builder

	sb.WriteString(titleStyle.Render("Create New Tool"))
	sb.WriteString("\n\n")

	if m.created {
		sb.WriteString(successStyle.Render("Tool Created Successfully!"))
		sb.WriteString("\n\n")
		sb.WriteString(fmt.Sprintf("Name: %s\n", m.tool.Name))
		sb.WriteString(fmt.Sprintf("Command: %s\n", m.tool.Command))
		sb.WriteString(fmt.Sprintf("Sandbox: %s\n", m.tool.Sandbox))
		sb.WriteString("\n")
		sb.WriteString(helpStyle.Render("Press esc to go back"))
	} else {
		// Name field
		style := normalStyle
		if m.focusedField == 0 {
			style = selectedStyle
		}
		sb.WriteString(style.Render("Name:"))
		sb.WriteString("\n")
		sb.WriteString(m.nameInput.View())
		sb.WriteString("\n\n")

		// Description field
		style = normalStyle
		if m.focusedField == 1 {
			style = selectedStyle
		}
		sb.WriteString(style.Render("Description:"))
		sb.WriteString("\n")
		sb.WriteString(m.descInput.View())
		sb.WriteString("\n\n")

		// Command field
		style = normalStyle
		if m.focusedField == 2 {
			style = selectedStyle
		}
		sb.WriteString(style.Render("Command:"))
		sb.WriteString("\n")
		sb.WriteString(m.commandInput.View())
		sb.WriteString("\n\n")

		// Args field
		style = normalStyle
		if m.focusedField == 3 {
			style = selectedStyle
		}
		sb.WriteString(style.Render("Allowed Arg Globs (one per line):"))
		sb.WriteString("\n")
		sb.WriteString(m.argsInput.View())
		sb.WriteString("\n\n")

		// Sandbox selector
		style = normalStyle
		if m.focusedField == 4 {
			style = selectedStyle
		}
		sb.WriteString(style.Render("Sandbox: "))
		for i, opt := range sandboxOptions {
			if i == m.sandboxIdx {
				sb.WriteString(selectedStyle.Render(fmt.Sprintf("[%s]", opt)))
			} else {
				sb.WriteString(normalStyle.Render(fmt.Sprintf(" %s ", opt)))
			}
			sb.WriteString(" ")
		}
		sb.WriteString("\n\n")

		// WASM binary (only visible for wasm sandbox)
		if sandboxOptions[m.sandboxIdx] == "wasm" {
			style = normalStyle
			if m.focusedField == 5 {
				style = selectedStyle
			}
			sb.WriteString(style.Render("WASM Binary Path:"))
			sb.WriteString("\n")
			sb.WriteString(m.wasmInput.View())
			sb.WriteString("\n\n")
		}

		if m.err != nil {
			sb.WriteString(errorStyle.Render(fmt.Sprintf("Error: %v", m.err)))
			sb.WriteString("\n\n")
		}

		sb.WriteString(helpStyle.Render("tab: next field • ctrl+p: change sandbox • ctrl+s: save • esc: cancel"))
	}

	return sb.String()
}

// ToolEditModel handles tool editing
type ToolEditModel struct {
	db           *db.DB
	tool         *db.Tool
	focusedField int
	nameInput    textinput.Model
	descInput    textinput.Model
	commandInput textinput.Model
	argsInput    textarea.Model
	sandboxIdx   int
	wasmInput    textinput.Model
	err          error
	saved        bool
}

// NewToolEditModel creates a new tool edit model
func NewToolEditModel(database *db.DB, tool *db.Tool) *ToolEditModel {
	nameInput := textinput.New()
	nameInput.SetValue(tool.Name)
	nameInput.Focus()
	nameInput.CharLimit = 50
	nameInput.Width = 40

	descInput := textinput.New()
	descInput.SetValue(tool.Description)
	descInput.CharLimit = 200
	descInput.Width = 60

	commandInput := textinput.New()
	commandInput.SetValue(tool.Command)
	commandInput.CharLimit = 200
	commandInput.Width = 60

	argsInput := textarea.New()
	argsInput.SetValue(strings.Join(tool.AllowedArgGlobs, "\n"))
	argsInput.CharLimit = 0
	argsInput.SetWidth(60)
	argsInput.SetHeight(5)

	wasmInput := textinput.New()
	wasmInput.SetValue(tool.WasmBinary)
	wasmInput.CharLimit = 200
	wasmInput.Width = 60

	// Find sandbox index
	sandboxIdx := 0
	for i, opt := range sandboxOptions {
		if opt == string(tool.Sandbox) {
			sandboxIdx = i
			break
		}
	}

	return &ToolEditModel{
		db:           database,
		tool:         tool,
		nameInput:    nameInput,
		descInput:    descInput,
		commandInput: commandInput,
		argsInput:    argsInput,
		sandboxIdx:   sandboxIdx,
		wasmInput:    wasmInput,
	}
}

// Init initializes the model
func (m *ToolEditModel) Init() tea.Cmd {
	return textinput.Blink
}

// Update handles messages
func (m *ToolEditModel) Update(msg tea.Msg) (*ToolEditModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "tab":
			m.focusedField = (m.focusedField + 1) % 6
			m.updateFocus()
			return m, nil
		case "shift+tab":
			m.focusedField = (m.focusedField - 1 + 6) % 6
			m.updateFocus()
			return m, nil
		case "ctrl+s":
			if !m.saved {
				return m, m.save
			}
		case "ctrl+p":
			if m.focusedField == 4 {
				m.sandboxIdx = (m.sandboxIdx + 1) % len(sandboxOptions)
			}
			return m, nil
		}
	case toolUpdatedMsg:
		m.saved = true
		return m, nil
	case errMsg:
		m.err = msg.err
		return m, nil
	}

	return m.updateInputs(msg)
}

func (m *ToolEditModel) updateFocus() {
	m.nameInput.Blur()
	m.descInput.Blur()
	m.commandInput.Blur()
	m.argsInput.Blur()
	m.wasmInput.Blur()

	switch m.focusedField {
	case 0:
		m.nameInput.Focus()
	case 1:
		m.descInput.Focus()
	case 2:
		m.commandInput.Focus()
	case 3:
		m.argsInput.Focus()
	case 5:
		m.wasmInput.Focus()
	}
}

func (m *ToolEditModel) updateInputs(msg tea.Msg) (*ToolEditModel, tea.Cmd) {
	var cmd tea.Cmd
	switch m.focusedField {
	case 0:
		m.nameInput, cmd = m.nameInput.Update(msg)
	case 1:
		m.descInput, cmd = m.descInput.Update(msg)
	case 2:
		m.commandInput, cmd = m.commandInput.Update(msg)
	case 3:
		m.argsInput, cmd = m.argsInput.Update(msg)
	case 5:
		m.wasmInput, cmd = m.wasmInput.Update(msg)
	}
	return m, cmd
}

func (m *ToolEditModel) save() tea.Msg {
	name := strings.TrimSpace(m.nameInput.Value())
	if name == "" {
		return errMsg{fmt.Errorf("tool name is required")}
	}

	command := strings.TrimSpace(m.commandInput.Value())
	if command == "" {
		return errMsg{fmt.Errorf("command is required")}
	}

	sandbox := db.SandboxType(sandboxOptions[m.sandboxIdx])
	if sandbox == db.SandboxTypeWasm && strings.TrimSpace(m.wasmInput.Value()) == "" {
		return errMsg{fmt.Errorf("WASM binary path is required for wasm sandbox")}
	}

	m.tool.Name = name
	m.tool.Description = strings.TrimSpace(m.descInput.Value())
	m.tool.Command = command
	m.tool.AllowedArgGlobs = parseLines(m.argsInput.Value())
	m.tool.Sandbox = sandbox
	m.tool.WasmBinary = strings.TrimSpace(m.wasmInput.Value())

	// Validate
	if err := policy.ValidateTool(m.tool); err != nil {
		return errMsg{err}
	}

	// Save
	if err := m.db.UpdateTool(m.tool); err != nil {
		return errMsg{err}
	}

	return toolUpdatedMsg{}
}

type toolUpdatedMsg struct{}

// View renders the model
func (m *ToolEditModel) View() string {
	var sb strings.Builder

	sb.WriteString(titleStyle.Render("Edit Tool"))
	sb.WriteString("\n\n")

	if m.saved {
		sb.WriteString(successStyle.Render("Tool Updated Successfully!"))
		sb.WriteString("\n\n")
		sb.WriteString(helpStyle.Render("Press esc to go back"))
	} else {
		// Name field
		style := normalStyle
		if m.focusedField == 0 {
			style = selectedStyle
		}
		sb.WriteString(style.Render("Name:"))
		sb.WriteString("\n")
		sb.WriteString(m.nameInput.View())
		sb.WriteString("\n\n")

		// Description field
		style = normalStyle
		if m.focusedField == 1 {
			style = selectedStyle
		}
		sb.WriteString(style.Render("Description:"))
		sb.WriteString("\n")
		sb.WriteString(m.descInput.View())
		sb.WriteString("\n\n")

		// Command field
		style = normalStyle
		if m.focusedField == 2 {
			style = selectedStyle
		}
		sb.WriteString(style.Render("Command:"))
		sb.WriteString("\n")
		sb.WriteString(m.commandInput.View())
		sb.WriteString("\n\n")

		// Args field
		style = normalStyle
		if m.focusedField == 3 {
			style = selectedStyle
		}
		sb.WriteString(style.Render("Allowed Arg Globs (one per line):"))
		sb.WriteString("\n")
		sb.WriteString(m.argsInput.View())
		sb.WriteString("\n\n")

		// Sandbox selector
		style = normalStyle
		if m.focusedField == 4 {
			style = selectedStyle
		}
		sb.WriteString(style.Render("Sandbox: "))
		for i, opt := range sandboxOptions {
			if i == m.sandboxIdx {
				sb.WriteString(selectedStyle.Render(fmt.Sprintf("[%s]", opt)))
			} else {
				sb.WriteString(normalStyle.Render(fmt.Sprintf(" %s ", opt)))
			}
			sb.WriteString(" ")
		}
		sb.WriteString("\n\n")

		// WASM binary (only visible for wasm sandbox)
		if sandboxOptions[m.sandboxIdx] == "wasm" {
			style = normalStyle
			if m.focusedField == 5 {
				style = selectedStyle
			}
			sb.WriteString(style.Render("WASM Binary Path:"))
			sb.WriteString("\n")
			sb.WriteString(m.wasmInput.View())
			sb.WriteString("\n\n")
		}

		if m.err != nil {
			sb.WriteString(errorStyle.Render(fmt.Sprintf("Error: %v", m.err)))
			sb.WriteString("\n\n")
		}

		sb.WriteString(helpStyle.Render("tab: next field • ctrl+p: change sandbox • ctrl+s: save • esc: cancel"))
	}

	return sb.String()
}

// ToolDetailModel handles tool detail view
type ToolDetailModel struct {
	db   *db.DB
	tool *db.Tool
	err  error
}

// NewToolDetailModel creates a new tool detail model
func NewToolDetailModel(database *db.DB, tool *db.Tool) *ToolDetailModel {
	return &ToolDetailModel{
		db:   database,
		tool: tool,
	}
}

// Init initializes the model
func (m *ToolDetailModel) Init() tea.Cmd {
	return nil
}

// Update handles messages
func (m *ToolDetailModel) Update(msg tea.Msg) (*ToolDetailModel, tea.Cmd) {
	switch msg := msg.(type) {
	case errMsg:
		m.err = msg.err
		return m, nil
	}
	return m, nil
}

// View renders the model
func (m *ToolDetailModel) View() string {
	var sb strings.Builder

	sb.WriteString(titleStyle.Render("Tool Details"))
	sb.WriteString("\n\n")

	sb.WriteString(fmt.Sprintf("Name:        %s\n", m.tool.Name))
	sb.WriteString(fmt.Sprintf("Description: %s\n", m.tool.Description))
	sb.WriteString(fmt.Sprintf("Command:     %s\n", m.tool.Command))
	sb.WriteString(fmt.Sprintf("Sandbox:     %s\n", m.tool.Sandbox))
	if m.tool.Sandbox == db.SandboxTypeWasm {
		sb.WriteString(fmt.Sprintf("WASM Binary: %s\n", m.tool.WasmBinary))
	}
	sb.WriteString(fmt.Sprintf("Created:     %s\n", m.tool.CreatedAt.Format("2006-01-02 15:04:05")))

	sb.WriteString("\n")
	sb.WriteString(titleStyle.Render("Allowed Arg Globs"))
	sb.WriteString("\n\n")
	if len(m.tool.AllowedArgGlobs) == 0 {
		sb.WriteString("(no restrictions - all arguments allowed)\n")
	} else {
		for _, glob := range m.tool.AllowedArgGlobs {
			sb.WriteString(fmt.Sprintf("  - %s\n", glob))
		}
	}

	sb.WriteString("\n")
	if m.err != nil {
		sb.WriteString(errorStyle.Render(fmt.Sprintf("Error: %v", m.err)))
		sb.WriteString("\n")
	}

	sb.WriteString(helpStyle.Render("\ne: edit • d: delete • esc: back"))

	return sb.String()
}
