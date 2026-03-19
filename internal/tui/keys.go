package tui

import "github.com/charmbracelet/bubbles/key"

type keyMap struct {
	Up            key.Binding
	Down          key.Binding
	Launch        key.Binding
	Toggle        key.Binding
	Sort          key.Binding
	SortReverse   key.Binding
	Focus         key.Binding
	Delete        key.Binding
	Batch         key.Binding
	Stream        key.Binding
	Enter         key.Binding
	Help          key.Binding
	Info          key.Binding
	Quit          key.Binding
	Right         key.Binding
	Left          key.Binding
	Escape        key.Binding
	CtrlD         key.Binding
	CtrlBackslash key.Binding
	Add           key.Binding
	Tab           key.Binding
	ShiftTab      key.Binding
	QuickRespond  key.Binding
}

var keys = keyMap{
	Up: key.NewBinding(
		key.WithKeys("up", "k"),
		key.WithHelp("j/k", "navigate"),
	),
	Down: key.NewBinding(
		key.WithKeys("down", "j"),
		key.WithHelp("", ""),
	),
	Launch: key.NewBinding(
		key.WithKeys("n"),
		key.WithHelp("n", "new agent"),
	),
	Toggle: key.NewBinding(
		key.WithKeys("t"),
		key.WithHelp("t", "cycle agent"),
	),
	Sort: key.NewBinding(
		key.WithKeys("s"),
		key.WithHelp("s", "sort column"),
	),
	SortReverse: key.NewBinding(
		key.WithKeys("S"),
		key.WithHelp("S", "reverse sort"),
	),
	Focus: key.NewBinding(
		key.WithKeys("f"),
		key.WithHelp("f", "focus agent tab"),
	),
	Delete: key.NewBinding(
		key.WithKeys("d"),
		key.WithHelp("d", "remove watch dir"),
	),
	Batch: key.NewBinding(
		key.WithKeys("B"),
		key.WithHelp("B", "batch send"),
	),
	Stream: key.NewBinding(
		key.WithKeys("v"),
		key.WithHelp("v", "toggle stream panel"),
	),
	Enter: key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "expand/collapse"),
	),
	Help: key.NewBinding(
		key.WithKeys("?"),
		key.WithHelp("?", "help"),
	),
	Quit: key.NewBinding(
		key.WithKeys("q", "ctrl+c"),
		key.WithHelp("q", "quit"),
	),
	Right: key.NewBinding(
		key.WithKeys("right"),
	),
	Left: key.NewBinding(
		key.WithKeys("left", "h"),
	),
	Escape: key.NewBinding(
		key.WithKeys("esc"),
		key.WithHelp("esc", "back"),
	),
	CtrlD: key.NewBinding(
		key.WithKeys("ctrl+d"),
		key.WithHelp("ctrl+d", "send prompt"),
	),
	CtrlBackslash: key.NewBinding(
		key.WithKeys("ctrl+\\"),
		key.WithHelp("ctrl+\\", "return from terminal"),
	),
	Info: key.NewBinding(
		key.WithKeys("I"),
		key.WithHelp("I", "settings"),
	),
	Add: key.NewBinding(
		key.WithKeys("a"),
		key.WithHelp("a", "add"),
	),
	Tab: key.NewBinding(
		key.WithKeys("tab"),
		key.WithHelp("tab", "next"),
	),
	ShiftTab: key.NewBinding(
		key.WithKeys("shift+tab"),
		key.WithHelp("shift+tab", "back"),
	),
	QuickRespond: key.NewBinding(
		key.WithKeys("ctrl+r"),
		key.WithHelp("ctrl+r", "arm quick response"),
	),
}
