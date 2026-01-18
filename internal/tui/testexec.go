package tui

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/takeshy/mcp-gatekeeper/internal/db"
	"github.com/takeshy/mcp-gatekeeper/internal/executor"
	"github.com/takeshy/mcp-gatekeeper/internal/policy"
)

// TestExecuteModel handles test execution
type TestExecuteModel struct {
	db           *db.DB
	width        int
	height       int
	apiKeyList   list.Model
	selectedKey  *db.APIKey
	selectedPol  *db.Policy
	inputs       []textinput.Model
	focusedField int
	result       *TestResult
	err          error
	step         int // 0: select key, 1: enter command, 2: show result
}

// TestResult represents the test execution result
type TestResult struct {
	Decision     *policy.Decision
	ExecResult   *executor.ExecuteResult
	NormalizedCmd *executor.NormalizedCommand
}

// NewTestExecuteModel creates a new test execution model
func NewTestExecuteModel(database *db.DB, width, height int) *TestExecuteModel {
	// API key list
	l := list.New(nil, list.NewDefaultDelegate(), width-4, height-12)
	l.Title = "Select API Key"
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(true)

	// Command inputs
	inputs := make([]textinput.Model, 3)

	cwd := textinput.New()
	cwd.Placeholder = "Working directory"
	cwd.Width = 60
	cwd.SetValue(os.Getenv("HOME"))
	inputs[0] = cwd

	cmd := textinput.New()
	cmd.Placeholder = "Command (e.g., ls)"
	cmd.Width = 60
	inputs[1] = cmd

	args := textinput.New()
	args.Placeholder = "Arguments (space-separated)"
	args.Width = 60
	inputs[2] = args

	return &TestExecuteModel{
		db:     database,
		width:  width,
		height: height,
		apiKeyList: l,
		inputs: inputs,
		step:   0,
	}
}

// Init initializes the model
func (m *TestExecuteModel) Init() tea.Cmd {
	return m.loadAPIKeys
}

func (m *TestExecuteModel) loadAPIKeys() tea.Msg {
	keys, err := m.db.ListAPIKeys()
	if err != nil {
		return errMsg{err}
	}
	return apiKeysMsg{keys}
}

// Update handles messages
func (m *TestExecuteModel) Update(msg tea.Msg) (*TestExecuteModel, tea.Cmd) {
	switch msg := msg.(type) {
	case apiKeysMsg:
		items := make([]list.Item, 0, len(msg.keys))
		for _, k := range msg.keys {
			if k.Status == "active" {
				items = append(items, APIKeyItem{key: k})
			}
		}
		m.apiKeyList.SetItems(items)
		return m, nil

	case errMsg:
		m.err = msg.err
		return m, nil

	case testResultMsg:
		m.result = msg.result
		m.step = 2
		return m, nil

	case tea.KeyMsg:
		switch m.step {
		case 0: // Select API key
			if key.Matches(msg, keys.Enter) {
				if item, ok := m.apiKeyList.SelectedItem().(APIKeyItem); ok {
					m.selectedKey = item.key
					pol, err := m.db.GetPolicyByAPIKeyID(item.key.ID)
					if err != nil || pol == nil {
						m.err = fmt.Errorf("no policy found for API key")
						return m, nil
					}
					m.selectedPol = pol
					m.step = 1
					m.inputs[0].Focus()
					return m, textinput.Blink
				}
			}
			var cmd tea.Cmd
			m.apiKeyList, cmd = m.apiKeyList.Update(msg)
			return m, cmd

		case 1: // Enter command
			switch msg.String() {
			case "tab", "shift+tab":
				if msg.String() == "tab" {
					m.focusedField = (m.focusedField + 1) % len(m.inputs)
				} else {
					m.focusedField = (m.focusedField - 1 + len(m.inputs)) % len(m.inputs)
				}
				for i := range m.inputs {
					if i == m.focusedField {
						m.inputs[i].Focus()
					} else {
						m.inputs[i].Blur()
					}
				}
				return m, nil
			case "enter":
				if m.inputs[1].Value() != "" { // cmd is required
					return m, m.runTest
				}
			case "esc":
				m.step = 0
				m.result = nil
				m.err = nil
				return m, nil
			}

			var cmd tea.Cmd
			m.inputs[m.focusedField], cmd = m.inputs[m.focusedField].Update(msg)
			return m, cmd

		case 2: // Show result
			switch msg.String() {
			case "esc", "enter":
				m.step = 1
				m.result = nil
				m.err = nil
				return m, nil
			}
		}
	}

	return m, nil
}

type testResultMsg struct {
	result *TestResult
}

func (m *TestExecuteModel) runTest() tea.Msg {
	cwd := m.inputs[0].Value()
	cmd := m.inputs[1].Value()
	argsStr := m.inputs[2].Value()

	var args []string
	if argsStr != "" {
		args = strings.Fields(argsStr)
	}

	// Normalize command
	normalizer := executor.NewNormalizer()
	normalized, err := normalizer.Normalize(cwd, cmd, args)
	if err != nil {
		return errMsg{err}
	}

	// Evaluate policy
	evaluator := policy.NewEvaluator()
	evalReq := &policy.EvaluateRequest{
		Cwd:     normalized.Cwd,
		Cmdline: normalized.Cmdline,
	}
	decision, err := evaluator.Evaluate(m.selectedPol, evalReq)
	if err != nil {
		return errMsg{err}
	}

	result := &TestResult{
		Decision:      decision,
		NormalizedCmd: normalized,
	}

	// Execute if allowed
	if decision.Allowed {
		exec := executor.NewExecutor(nil)
		execResult, err := exec.Execute(context.Background(), normalized.Cwd, normalized.Cmd, args, os.Environ())
		if err != nil {
			return errMsg{err}
		}
		result.ExecResult = execResult
	}

	return testResultMsg{result: result}
}

// View renders the model
func (m *TestExecuteModel) View() string {
	var sb strings.Builder

	sb.WriteString(titleStyle.Render("Test Command Execution"))
	sb.WriteString("\n\n")

	switch m.step {
	case 0:
		sb.WriteString(m.apiKeyList.View())
		sb.WriteString("\n")
		sb.WriteString(helpStyle.Render("enter: select • esc: back"))

	case 1:
		sb.WriteString(fmt.Sprintf("API Key: %s (ID: %d)\n\n", m.selectedKey.Name, m.selectedKey.ID))

		fieldNames := []string{"Working Directory:", "Command:", "Arguments:"}
		for i, input := range m.inputs {
			style := normalStyle
			if i == m.focusedField {
				style = selectedStyle
			}
			sb.WriteString(style.Render(fieldNames[i]))
			sb.WriteString("\n")
			sb.WriteString(input.View())
			sb.WriteString("\n\n")
		}

		if m.err != nil {
			sb.WriteString(errorStyle.Render(fmt.Sprintf("Error: %v", m.err)))
			sb.WriteString("\n\n")
		}

		sb.WriteString(helpStyle.Render("tab: next field • enter: execute • esc: back"))

	case 2:
		sb.WriteString(fmt.Sprintf("API Key: %s\n\n", m.selectedKey.Name))

		sb.WriteString(titleStyle.Render("Normalized Command"))
		sb.WriteString("\n")
		sb.WriteString(fmt.Sprintf("CWD:     %s\n", m.result.NormalizedCmd.Cwd))
		sb.WriteString(fmt.Sprintf("Cmd:     %s\n", m.result.NormalizedCmd.Cmd))
		sb.WriteString(fmt.Sprintf("Cmdline: %s\n", m.result.NormalizedCmd.Cmdline))
		sb.WriteString("\n")

		sb.WriteString(titleStyle.Render("Policy Decision"))
		sb.WriteString("\n")
		if m.result.Decision.Allowed {
			sb.WriteString(successStyle.Render("ALLOWED"))
		} else {
			sb.WriteString(errorStyle.Render("DENIED"))
		}
		sb.WriteString(fmt.Sprintf("\nReason: %s\n", m.result.Decision.Reason))
		sb.WriteString(fmt.Sprintf("Rules:  %v\n", m.result.Decision.MatchedRules))
		sb.WriteString("\n")

		if m.result.ExecResult != nil {
			sb.WriteString(titleStyle.Render("Execution Result"))
			sb.WriteString("\n")
			sb.WriteString(fmt.Sprintf("Exit Code: %d\n", m.result.ExecResult.ExitCode))
			sb.WriteString(fmt.Sprintf("Duration:  %dms\n", m.result.ExecResult.DurationMs))

			if m.result.ExecResult.Stdout != "" {
				sb.WriteString("\nStdout:\n")
				sb.WriteString(boxStyle.Render(truncate(m.result.ExecResult.Stdout, 500)))
				sb.WriteString("\n")
			}
			if m.result.ExecResult.Stderr != "" {
				sb.WriteString("\nStderr:\n")
				sb.WriteString(boxStyle.Render(truncate(m.result.ExecResult.Stderr, 500)))
				sb.WriteString("\n")
			}
		}

		sb.WriteString("\n")
		sb.WriteString(helpStyle.Render("esc/enter: try another command"))
	}

	return sb.String()
}

// App screen handler for test execute
func (a *App) updateTestExecute(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if key.Matches(msg, keys.Back) && a.testExecute.step == 0 {
			a.popScreen()
			return a, nil
		}
	}

	var cmd tea.Cmd
	a.testExecute, cmd = a.testExecute.Update(msg)
	return a, cmd
}

func (a *App) viewTestExecute() string {
	if a.testExecute == nil {
		return "Loading..."
	}
	return a.testExecute.View()
}
