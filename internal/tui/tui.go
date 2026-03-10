package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/p-society/raag/internal/library"
	"github.com/p-society/raag/internal/network"
	"github.com/p-society/raag/internal/player"
	"github.com/p-society/raag/internal/playlist"
)

func Start(lib *library.Library, p *player.Player, net *network.NetworkManager, pm *playlist.Manager) error {
	m := NewModel(lib, p, net, pm)
	program := tea.NewProgram(m, tea.WithAltScreen())
	_, err := program.Run()
	return err
}
