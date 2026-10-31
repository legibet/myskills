package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// modal is a dialog drawn over the current view: a confirmation, or a
// checklist when items is not nil. It takes every key while open.
type modal struct {
	title string
	body  string

	// Confirmation.
	action string         // label of the confirming key, e.g. "Remove"
	danger bool           // the action destroys data
	onYes  func() tea.Cmd // runs when confirmed

	// Checklist.
	items    []checkItem
	cursor   int
	onApply  func(checked []bool) tea.Cmd
	validate func(checked []bool) string // optional; a message keeps the modal open
	problem  string
}

type checkItem struct {
	label, detail string
	checked       bool
}

func confirmModal(title, body, action string, danger bool, onYes func() tea.Cmd) *modal {
	return &modal{title: title, body: body, action: action, danger: danger, onYes: onYes}
}

func checklistModal(title, body string, items []checkItem, onApply func([]bool) tea.Cmd) *modal {
	return &modal{title: title, body: body, items: items, onApply: onApply}
}

// update handles a key and reports whether the modal closed.
func (m *modal) update(msg tea.KeyPressMsg) (closed bool, cmd tea.Cmd) {
	k := msg.String()
	if m.items == nil {
		switch k {
		case "y", "enter":
			return true, m.onYes()
		case "n", "esc", "q":
			return true, nil
		}
		return false, nil
	}
	switch k {
	case "up", "k", "ctrl+p":
		m.cursor = max(0, m.cursor-1)
	case "down", "j", "ctrl+n":
		m.cursor = min(len(m.items)-1, m.cursor+1)
	case "space", "x":
		m.items[m.cursor].checked = !m.items[m.cursor].checked
		m.problem = ""
	case "enter":
		checked := make([]bool, len(m.items))
		for i, it := range m.items {
			checked[i] = it.checked
		}
		if m.validate != nil {
			if m.problem = m.validate(checked); m.problem != "" {
				return false, nil
			}
		}
		return true, m.onApply(checked)
	case "esc", "q":
		return true, nil
	}
	return false, nil
}

// view renders the modal box for a screen of the given width.
func (m *modal) view(t theme, screenW int) string {
	w := min(68, screenW-4)
	inner := w - t.modal.GetHorizontalFrameSize()
	wrap := lipgloss.NewStyle().Width(inner)

	parts := []string{t.bold.Render(m.title)}
	if m.body != "" {
		parts = append(parts, "", wrap.Render(m.body))
	}
	if m.items != nil {
		parts = append(parts, "")
		for i, it := range m.items {
			parts = append(parts, m.itemLine(t, i, it, inner))
		}
		if m.problem != "" {
			parts = append(parts, "", t.warn.Render(m.problem))
		}
		parts = append(parts, "", keyHints(t, "space", "toggle", "enter", "apply", "esc", "cancel"))
	} else {
		label := t.accented.Bold(true).Render(m.action)
		if m.danger {
			label = t.bad.Bold(true).Render(m.action)
		}
		parts = append(parts, "", t.bold.Render("y")+" "+label+"   "+keyHints(t, "esc", "cancel"))
	}
	return t.modal.Width(w).Render(strings.Join(parts, "\n"))
}

func (m *modal) itemLine(t theme, i int, it checkItem, w int) string {
	pointer, box := "  ", t.hint.Render("[ ]")
	if it.checked {
		box = t.hint.Render("[") + t.accented.Render("✓") + t.hint.Render("]")
	}
	label := t.text.Render(it.label)
	if i == m.cursor {
		pointer = t.accented.Render("› ")
		label = t.bold.Render(it.label)
	}
	line := pointer + box + " " + label
	if it.detail != "" {
		gap := max(2, w-lipgloss.Width(line)-lipgloss.Width(it.detail))
		line += strings.Repeat(" ", gap) + t.dim.Render(it.detail)
	}
	return truncate(line, w)
}

// keyHints renders "key desc" pairs in the help bar style.
func keyHints(t theme, pairs ...string) string {
	var out []string
	for i := 0; i+1 < len(pairs); i += 2 {
		out = append(out, t.bold.Render(pairs[i])+" "+t.dim.Render(pairs[i+1]))
	}
	return strings.Join(out, "   ")
}
