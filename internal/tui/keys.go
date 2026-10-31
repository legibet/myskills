package tui

import "charm.land/bubbles/v2/key"

func bind(keys []string, helpKey, desc string) key.Binding {
	return key.NewBinding(key.WithKeys(keys...), key.WithHelp(helpKey, desc))
}

// skillsKeys are the Skills view bindings. Descriptions of the row actions
// follow the highlighted row (see app.syncKeys).
type skillsKeys struct {
	Up, Down, Top, Bottom, PageUp, PageDown key.Binding
	NextTab, PrevTab                        key.Binding
	Toggle, Copy, Agents, Defaults, Scope   key.Binding
	Check, Update, Remove, Edit             key.Binding
	Filter, Read, ScrollDown, ScrollUp      key.Binding
	Find, Help, Quit                        key.Binding
}

func newSkillsKeys() skillsKeys {
	return skillsKeys{
		Up:         bind([]string{"up", "k"}, "↑/k", "up"),
		Down:       bind([]string{"down", "j"}, "↓/j", "down"),
		Top:        bind([]string{"home", "g"}, "g", "top"),
		Bottom:     bind([]string{"end", "G"}, "G", "bottom"),
		PageUp:     bind([]string{"pgup"}, "pgup", "page up"),
		PageDown:   bind([]string{"pgdown"}, "pgdn", "page down"),
		NextTab:    bind([]string{"tab"}, "tab", "next tab"),
		PrevTab:    bind([]string{"shift+tab"}, "shift+tab", "previous tab"),
		Toggle:     bind([]string{"space"}, "space", "enable"),
		Copy:       bind([]string{"c"}, "c", "copy"),
		Agents:     bind([]string{"a"}, "a", "agents"),
		Defaults:   bind([]string{"A"}, "A", "default agents"),
		Scope:      bind([]string{"s"}, "s", "scope"),
		Check:      bind([]string{"r"}, "r", "check updates"),
		Update:     bind([]string{"u"}, "u", "update"),
		Remove:     bind([]string{"x"}, "x", "remove"),
		Edit:       bind([]string{"e"}, "e", "edit"),
		Filter:     bind([]string{"/"}, "/", "filter"),
		Read:       bind([]string{"enter"}, "enter", "read"),
		ScrollDown: bind([]string{"ctrl+d"}, "ctrl+d", "scroll preview"),
		ScrollUp:   bind([]string{"ctrl+u"}, "ctrl+u", "scroll preview up"),
		Find:       bind([]string{"f"}, "f", "find"),
		Help:       bind([]string{"?"}, "?", "help"),
		Quit:       bind([]string{"q", "ctrl+c"}, "q", "quit"),
	}
}

func (k skillsKeys) ShortHelp() []key.Binding {
	return []key.Binding{k.Toggle, k.Copy, k.Agents, k.Scope, k.Update, k.Remove, k.Filter, k.Find, k.Help}
}

func (k skillsKeys) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.Top, k.Bottom, k.NextTab, k.PrevTab, k.Read, k.ScrollDown, k.ScrollUp},
		{k.Toggle, k.Copy, k.Agents, k.Defaults, k.Scope},
		{k.Check, k.Update, k.Remove, k.Edit, k.Filter},
		{k.Find, k.Help, k.Quit},
	}
}

// filterKeys apply while the Skills filter input is focused.
type filterKeys struct {
	Up, Down, Apply, Clear key.Binding
}

func newFilterKeys() filterKeys {
	return filterKeys{
		Up:    bind([]string{"up", "ctrl+p"}, "↑", "up"),
		Down:  bind([]string{"down", "ctrl+n"}, "↓", "down"),
		Apply: bind([]string{"enter"}, "enter", "keep filter"),
		Clear: bind([]string{"esc"}, "esc", "clear"),
	}
}

func (k filterKeys) ShortHelp() []key.Binding {
	return []key.Binding{k.Apply, k.Clear, k.Up, k.Down}
}

func (k filterKeys) FullHelp() [][]key.Binding {
	return [][]key.Binding{k.ShortHelp()}
}

// readerKeys apply while a preview is open full screen.
type readerKeys struct {
	Up, Down, HalfDown, HalfUp, PageDown, PageUp, Top, Bottom, Back, Quit key.Binding
}

func newReaderKeys() readerKeys {
	return readerKeys{
		Up:       bind([]string{"up", "k"}, "↑/k", "up"),
		Down:     bind([]string{"down", "j"}, "↓/j", "down"),
		HalfDown: bind([]string{"ctrl+d"}, "ctrl+d", "half page down"),
		HalfUp:   bind([]string{"ctrl+u"}, "ctrl+u", "half page up"),
		PageDown: bind([]string{"pgdown", "space", "f"}, "space", "page down"),
		PageUp:   bind([]string{"pgup", "b"}, "b", "page up"),
		Top:      bind([]string{"home", "g"}, "g", "top"),
		Bottom:   bind([]string{"end", "G"}, "G", "bottom"),
		Back:     bind([]string{"esc", "enter"}, "esc", "back"),
		Quit:     bind([]string{"q", "ctrl+c"}, "q", "quit"),
	}
}

func (k readerKeys) ShortHelp() []key.Binding {
	return []key.Binding{k.Back, k.Down, k.Up, k.PageDown, k.PageUp, k.Top, k.Bottom, k.Quit}
}

func (k readerKeys) FullHelp() [][]key.Binding {
	return [][]key.Binding{k.ShortHelp()}
}

// findKeys are the Find view bindings; the query input takes all other keys.
type findKeys struct {
	Up, Down, PageUp, PageDown, Enter, Back key.Binding
	Open, Retry, ScrollDown, ScrollUp, Quit key.Binding
}

func newFindKeys() findKeys {
	return findKeys{
		Up:         bind([]string{"up", "ctrl+p"}, "↑", "up"),
		Down:       bind([]string{"down", "ctrl+n"}, "↓", "down"),
		PageUp:     bind([]string{"pgup"}, "pgup", "page up"),
		PageDown:   bind([]string{"pgdown"}, "pgdn", "page down"),
		Enter:      bind([]string{"enter"}, "enter", "add"),
		Back:       bind([]string{"esc"}, "esc", "back"),
		Open:       bind([]string{"ctrl+o"}, "ctrl+o", "open in browser"),
		Retry:      bind([]string{"ctrl+r"}, "ctrl+r", "retry"),
		ScrollDown: bind([]string{"ctrl+d"}, "ctrl+d", "scroll preview"),
		ScrollUp:   bind([]string{"ctrl+u"}, "ctrl+u", "scroll preview up"),
		Quit:       bind([]string{"ctrl+c"}, "ctrl+c", "quit"),
	}
}

func (k findKeys) ShortHelp() []key.Binding {
	return []key.Binding{k.Enter, k.Back, k.Up, k.Down, k.Retry, k.Open, k.ScrollDown, k.Quit}
}

func (k findKeys) FullHelp() [][]key.Binding {
	return [][]key.Binding{k.ShortHelp()}
}

// pickerKeys are the add picker bindings.
type pickerKeys struct {
	Up, Down, Toggle, All, Install, Cancel, ScrollDown, ScrollUp key.Binding
}

func newPickerKeys() pickerKeys {
	return pickerKeys{
		Up:         bind([]string{"up", "k"}, "↑/k", "up"),
		Down:       bind([]string{"down", "j"}, "↓/j", "down"),
		Toggle:     bind([]string{"space"}, "space", "toggle"),
		All:        bind([]string{"ctrl+a"}, "ctrl+a", "all"),
		Install:    bind([]string{"enter"}, "enter", "add"),
		Cancel:     bind([]string{"esc", "q", "ctrl+c"}, "esc", "cancel"),
		ScrollDown: bind([]string{"ctrl+d"}, "ctrl+d", "scroll preview"),
		ScrollUp:   bind([]string{"ctrl+u"}, "ctrl+u", "scroll preview up"),
	}
}

func (k pickerKeys) ShortHelp() []key.Binding {
	return []key.Binding{k.Toggle, k.All, k.Install, k.Cancel, k.Up, k.Down, k.ScrollDown}
}

func (k pickerKeys) FullHelp() [][]key.Binding {
	return [][]key.Binding{k.ShortHelp()}
}
