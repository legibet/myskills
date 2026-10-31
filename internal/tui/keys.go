package tui

import "charm.land/bubbles/v2/key"

func bind(keys []string, helpKey, desc string) key.Binding {
	return key.NewBinding(key.WithKeys(keys...), key.WithHelp(helpKey, desc))
}

// Short help lists bindings by priority: the help bar shows as many as fit,
// from the first, and keeps the help key (see fitHelp).

// skillsKeys are the Skills view bindings. Descriptions of the row actions
// follow the highlighted row (see app.syncKeys).
type skillsKeys struct {
	Up, Down, Top, Bottom, PageUp, PageDown key.Binding
	NextTab, PrevTab                        key.Binding
	Toggle, Copy, Agents, Defaults, Scope   key.Binding
	Update, CheckAll, Reload, Remove, Edit  key.Binding
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
		Update:     bind([]string{"u"}, "u", "update"),
		CheckAll:   bind([]string{"U"}, "U", "check updates"),
		Reload:     bind([]string{"r"}, "r", "reload"),
		Remove:     bind([]string{"d", "x"}, "d", "remove"),
		Edit:       bind([]string{"e"}, "e", "edit"),
		Filter:     bind([]string{"/"}, "/", "filter"),
		Read:       bind([]string{"enter", "l", "right"}, "enter", "read"),
		ScrollDown: bind([]string{"ctrl+d"}, "ctrl+d", "scroll preview"),
		ScrollUp:   bind([]string{"ctrl+u"}, "ctrl+u", "scroll preview up"),
		Find:       bind([]string{"f"}, "f", "find"),
		Help:       bind([]string{"?"}, "?", "help"),
		Quit:       bind([]string{"q", "ctrl+c"}, "q", "quit"),
	}
}

func (k skillsKeys) ShortHelp() []key.Binding {
	return []key.Binding{k.Toggle, k.Copy, k.Read, k.Filter, k.Find, k.Update, k.Agents, k.Scope, k.Remove}
}

func (k skillsKeys) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.Top, k.Bottom, k.NextTab, k.PrevTab, k.Read, k.ScrollDown, k.ScrollUp},
		{k.Toggle, k.Copy, k.Agents, k.Defaults, k.Scope},
		{k.Update, k.CheckAll, k.Reload, k.Remove, k.Edit, k.Filter},
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
		Back:     bind([]string{"esc", "h", "left"}, "esc", "back"),
		Quit:     bind([]string{"q", "ctrl+c"}, "q", "quit"),
	}
}

func (k readerKeys) ShortHelp() []key.Binding {
	return []key.Binding{k.Back, k.Down, k.Up, k.PageDown, k.PageUp, k.Top, k.Bottom, k.Quit}
}

// queryKeys apply while the Find query input is focused; it takes all other
// keys.
type queryKeys struct {
	Submit, Results, Back key.Binding
}

func newQueryKeys() queryKeys {
	return queryKeys{
		Submit:  bind([]string{"enter"}, "enter", "search"),
		Results: bind([]string{"down"}, "↓", "results"),
		Back:    bind([]string{"esc"}, "esc", "results"),
	}
}

func (k queryKeys) ShortHelp() []key.Binding {
	return []key.Binding{k.Submit, k.Results, k.Back}
}

// findKeys are the Find view bindings while the list is focused.
type findKeys struct {
	Up, Down, Top, Bottom, PageUp, PageDown key.Binding
	Read, Add, Open, Search, Retry, Leave   key.Binding
	ScrollDown, ScrollUp, Back, Help, Quit  key.Binding
}

func newFindKeys() findKeys {
	return findKeys{
		Up:         bind([]string{"up", "k"}, "↑/k", "up"),
		Down:       bind([]string{"down", "j"}, "↓/j", "down"),
		Top:        bind([]string{"home", "g"}, "g", "top"),
		Bottom:     bind([]string{"end", "G"}, "G", "bottom"),
		PageUp:     bind([]string{"pgup"}, "pgup", "page up"),
		PageDown:   bind([]string{"pgdown"}, "pgdn", "page down"),
		Read:       bind([]string{"enter", "l", "right"}, "enter", "read"),
		Add:        bind([]string{"space"}, "space", "add"),
		Open:       bind([]string{"o"}, "o", "open in browser"),
		Search:     bind([]string{"/"}, "/", "search"),
		Retry:      bind([]string{"r"}, "r", "retry"),
		Leave:      bind([]string{"h", "left"}, "h", "back to results"),
		ScrollDown: bind([]string{"ctrl+d"}, "ctrl+d", "scroll preview"),
		ScrollUp:   bind([]string{"ctrl+u"}, "ctrl+u", "scroll preview up"),
		Back:       bind([]string{"esc"}, "esc", "skills"),
		Help:       bind([]string{"?"}, "?", "help"),
		Quit:       bind([]string{"q", "ctrl+c"}, "q", "quit"),
	}
}

func (k findKeys) ShortHelp() []key.Binding {
	return []key.Binding{k.Add, k.Read, k.Search, k.Retry, k.Back, k.Open}
}

func (k findKeys) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.Top, k.Bottom, k.PageUp, k.PageDown},
		{k.Read, k.Add, k.Open, k.Retry, k.ScrollDown, k.ScrollUp},
		{k.Search, k.Leave, k.Back, k.Help, k.Quit},
	}
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
