package main

import (
	"flag"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/takeshy/mcp-gatekeeper/internal/db"
	"github.com/takeshy/mcp-gatekeeper/internal/tui"
)

func main() {
	var (
		dbPath = flag.String("db", "gatekeeper.db", "SQLite database path")
	)
	flag.Parse()

	// Open database
	database, err := db.Open(*dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to open database: %v\n", err)
		os.Exit(1)
	}
	defer database.Close()

	// Create and run TUI app
	app := tui.NewApp(database)
	p := tea.NewProgram(app, tea.WithAltScreen())

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error running TUI: %v\n", err)
		os.Exit(1)
	}
}
