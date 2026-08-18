package tui

import "charm.land/bubbles/v2/key"

type KeyMap struct {
	Quit      key.Binding
	Help      key.Binding
	Tab       key.Binding
	Search    key.Binding
	PlayPause key.Binding
	Next      key.Binding
	Prev      key.Binding
	Stop      key.Binding
	SeekFwd   key.Binding
	SeekBack  key.Binding
	VolUp     key.Binding
	VolDown   key.Binding
	Enter     key.Binding
	Delete    key.Binding
	Refresh   key.Binding
	Shuffle   key.Binding
	Repeat    key.Binding
	Lyrics    key.Binding
}

func DefaultKeyMap() KeyMap {
	return KeyMap{
		Quit:      key.NewBinding(key.WithKeys("q", "ctrl+c")),
		Help:      key.NewBinding(key.WithKeys("?")),
		Tab:       key.NewBinding(key.WithKeys("tab")),
		Search:    key.NewBinding(key.WithKeys("/")),
		PlayPause: key.NewBinding(key.WithKeys(" ")),
		Next:      key.NewBinding(key.WithKeys("n")),
		Prev:      key.NewBinding(key.WithKeys("p")),
		Stop:      key.NewBinding(key.WithKeys("x")),
		SeekFwd:   key.NewBinding(key.WithKeys("f")),
		SeekBack:  key.NewBinding(key.WithKeys("b")),
		VolUp:     key.NewBinding(key.WithKeys("+", "=")),
		VolDown:   key.NewBinding(key.WithKeys("-")),
		Enter:     key.NewBinding(key.WithKeys("enter")),
		Delete:    key.NewBinding(key.WithKeys("d")),
		Refresh:   key.NewBinding(key.WithKeys("r")),
		Shuffle:   key.NewBinding(key.WithKeys("s")),
		Repeat:    key.NewBinding(key.WithKeys("R")),
		Lyrics:    key.NewBinding(key.WithKeys("L")),
	}
}
