package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/takeshy/mcp-gatekeeper/internal/db"
)

// AuditLogItem represents an audit log in the list
type AuditLogItem struct {
	log *db.AuditLog
}

func (i AuditLogItem) Title() string {
	status := "✓"
	if i.log.Decision == db.DecisionDeny {
		status = "✗"
	}
	cmd := i.log.RequestedCmd
	if len(cmd) > 30 {
		cmd = cmd[:27] + "..."
	}
	return fmt.Sprintf("%s %s", status, cmd)
}
func (i AuditLogItem) Description() string {
	return fmt.Sprintf("%s | %s", i.log.CreatedAt.Format("2006-01-02 15:04:05"), i.log.Decision)
}
func (i AuditLogItem) FilterValue() string { return i.log.RequestedCmd }

// AuditLogListModel handles the audit log list view
type AuditLogListModel struct {
	db     *db.DB
	list   list.Model
	width  int
	height int
	err    error
	page   int
	limit  int
}

// NewAuditLogListModel creates a new audit log list model
func NewAuditLogListModel(database *db.DB, width, height int) *AuditLogListModel {
	l := list.New(nil, list.NewDefaultDelegate(), width-4, height-8)
	l.Title = "Audit Logs"
	l.SetShowStatusBar(true)
	l.SetFilteringEnabled(true)

	return &AuditLogListModel{
		db:     database,
		list:   l,
		width:  width,
		height: height,
		page:   0,
		limit:  50,
	}
}

// Init initializes the model
func (m *AuditLogListModel) Init() tea.Cmd {
	return m.loadAuditLogs
}

func (m *AuditLogListModel) loadAuditLogs() tea.Msg {
	logs, err := m.db.ListAuditLogs(m.limit, m.page*m.limit)
	if err != nil {
		return errMsg{err}
	}
	return auditLogsMsg{logs}
}

type auditLogsMsg struct {
	logs []*db.AuditLog
}

// Update handles messages
func (m *AuditLogListModel) Update(msg tea.Msg) (*AuditLogListModel, tea.Cmd) {
	switch msg := msg.(type) {
	case auditLogsMsg:
		items := make([]list.Item, len(msg.logs))
		for i, log := range msg.logs {
			items[i] = AuditLogItem{log: log}
		}
		m.list.SetItems(items)
		return m, nil

	case errMsg:
		m.err = msg.err
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "n":
			m.page++
			return m, m.loadAuditLogs
		case "p":
			if m.page > 0 {
				m.page--
				return m, m.loadAuditLogs
			}
		}
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

// View renders the model
func (m *AuditLogListModel) View() string {
	if m.err != nil {
		return errorStyle.Render(fmt.Sprintf("Error: %v", m.err))
	}
	return lipgloss.JoinVertical(
		lipgloss.Left,
		m.list.View(),
		helpStyle.Render(fmt.Sprintf("\n  Page %d | n: next page • p: prev page • enter: details • esc: back", m.page+1)),
	)
}

// SelectedLog returns the selected audit log
func (m *AuditLogListModel) SelectedLog() *db.AuditLog {
	if item, ok := m.list.SelectedItem().(AuditLogItem); ok {
		return item.log
	}
	return nil
}

// AuditLogDetailModel handles audit log detail view
type AuditLogDetailModel struct {
	db  *db.DB
	log *db.AuditLog
	err error
}

// NewAuditLogDetailModel creates a new audit log detail model
func NewAuditLogDetailModel(database *db.DB, log *db.AuditLog) *AuditLogDetailModel {
	return &AuditLogDetailModel{
		db:  database,
		log: log,
	}
}

// Init initializes the model
func (m *AuditLogDetailModel) Init() tea.Cmd {
	return nil
}

// Update handles messages
func (m *AuditLogDetailModel) Update(msg tea.Msg) (*AuditLogDetailModel, tea.Cmd) {
	return m, nil
}

// View renders the model
func (m *AuditLogDetailModel) View() string {
	var sb strings.Builder

	sb.WriteString(titleStyle.Render("Audit Log Details"))
	sb.WriteString("\n\n")

	decision := successStyle.Render("ALLOWED")
	if m.log.Decision == db.DecisionDeny {
		decision = errorStyle.Render("DENIED")
	}

	sb.WriteString(fmt.Sprintf("Decision:     %s\n", decision))
	sb.WriteString(fmt.Sprintf("Time:         %s\n", m.log.CreatedAt.Format("2006-01-02 15:04:05")))
	sb.WriteString(fmt.Sprintf("API Key ID:   %d\n", m.log.APIKeyID))
	sb.WriteString("\n")

	sb.WriteString(titleStyle.Render("Request"))
	sb.WriteString("\n")
	sb.WriteString(fmt.Sprintf("CWD:    %s\n", m.log.RequestedCwd))
	sb.WriteString(fmt.Sprintf("Cmd:    %s\n", m.log.RequestedCmd))
	sb.WriteString(fmt.Sprintf("Args:   %v\n", m.log.RequestedArgs))
	sb.WriteString("\n")

	sb.WriteString(titleStyle.Render("Normalized"))
	sb.WriteString("\n")
	sb.WriteString(fmt.Sprintf("CWD:     %s\n", m.log.NormalizedCwd))
	sb.WriteString(fmt.Sprintf("Cmdline: %s\n", m.log.NormalizedCmdline))
	sb.WriteString("\n")

	sb.WriteString(fmt.Sprintf("Matched Rules: %v\n", m.log.MatchedRules))

	if m.log.Decision == db.DecisionAllow {
		sb.WriteString("\n")
		sb.WriteString(titleStyle.Render("Execution Result"))
		sb.WriteString("\n")
		if m.log.ExitCode.Valid {
			sb.WriteString(fmt.Sprintf("Exit Code:   %d\n", m.log.ExitCode.Int64))
		}
		if m.log.DurationMs.Valid {
			sb.WriteString(fmt.Sprintf("Duration:    %dms\n", m.log.DurationMs.Int64))
		}
		if m.log.Stdout != "" {
			sb.WriteString("\nStdout:\n")
			sb.WriteString(boxStyle.Render(truncate(m.log.Stdout, 500)))
			sb.WriteString("\n")
		}
		if m.log.Stderr != "" {
			sb.WriteString("\nStderr:\n")
			sb.WriteString(boxStyle.Render(truncate(m.log.Stderr, 500)))
			sb.WriteString("\n")
		}
	}

	sb.WriteString("\n")
	sb.WriteString(helpStyle.Render("esc: back"))

	return sb.String()
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// App screen handlers for audit logs
func (a *App) updateAuditLogs(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if key.Matches(msg, keys.Back) {
			a.popScreen()
			return a, nil
		}
		if key.Matches(msg, keys.Enter) {
			if selected := a.auditLogList.SelectedLog(); selected != nil {
				a.auditLogDetail = NewAuditLogDetailModel(a.db, selected)
				a.pushScreen(ScreenAuditLogDetail)
				return a, a.auditLogDetail.Init()
			}
		}
	}

	var cmd tea.Cmd
	a.auditLogList, cmd = a.auditLogList.Update(msg)
	return a, cmd
}

func (a *App) updateAuditLogDetail(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if key.Matches(msg, keys.Back) {
			a.popScreen()
			return a, nil
		}
	}

	var cmd tea.Cmd
	a.auditLogDetail, cmd = a.auditLogDetail.Update(msg)
	return a, cmd
}

func (a *App) viewAuditLogs() string {
	if a.auditLogList == nil {
		return "Loading..."
	}
	return a.auditLogList.View()
}

func (a *App) viewAuditLogDetail() string {
	if a.auditLogDetail == nil {
		return "Loading..."
	}
	return a.auditLogDetail.View()
}
