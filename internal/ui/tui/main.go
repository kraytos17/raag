package tui

import (
	"fmt"
	"os"

	tea "charm.land/bubbletea/v2"
)

func Run(socketPath string) error {
	m := NewModel(socketPath)
	p := tea.NewProgram(m)
	m.Program = p
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error running TUI: %v\n", err)
		return err
	}
	return nil
}
