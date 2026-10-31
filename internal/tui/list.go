package tui

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// listCursor is the highlighted row and the first visible row of a list.
type listCursor struct {
	cursor, offset int
}

// move moves the cursor by delta within n rows.
func (l *listCursor) move(delta, n int) {
	l.cursor = max(0, min(l.cursor+delta, n-1))
}

// fit clamps the cursor to n rows and scrolls so that it is visible in a
// window of h rows. The offset only moves when the cursor leaves the window.
func (l *listCursor) fit(n, h int) {
	l.cursor = max(0, min(l.cursor, n-1))
	if h <= 0 {
		return
	}
	switch {
	case l.cursor < l.offset:
		l.offset = l.cursor
	case l.cursor >= l.offset+h:
		l.offset = l.cursor - h + 1
	}
	l.offset = max(0, min(l.offset, n-h))
}

// window returns the visible row range for a window of h rows.
func (l listCursor) window(n, h int) (start, end int) {
	return l.offset, min(n, l.offset+h)
}

// seg is a piece of a row: plain text and its style. hl lists rune indexes
// to draw with hlStyle (fuzzy matches).
type seg struct {
	text    string
	style   lipgloss.Style
	hl      []int
	hlStyle lipgloss.Style
}

// cursorSeg is the first cell of a list row: a bar on the highlighted row, so
// that the cursor shows without colors too (NO_COLOR drops the background).
func cursorSeg(t theme, selected bool) seg {
	if selected {
		return seg{text: "▌", style: t.accented}
	}
	return seg{text: " "}
}

// layoutRow renders a list row of exactly w cells: left segments in order,
// truncated when space runs out, and right segments aligned to the right
// edge. When the row does not fit, the first right segments are dropped until
// the right side takes at most 40% of the row, so put the most important ones
// last. bg, when not nil, fills the whole row.
func layoutRow(w int, bg color.Color, left, right []seg) string {
	paint := func(st lipgloss.Style, s string) string {
		if bg != nil {
			st = st.Background(bg)
		}
		return st.Render(s)
	}
	lw, rw := segsWidth(left), segsWidth(right)
	for len(right) > 0 && lw+rw+1 > w && (rw+1)*5 > w*2 {
		right = right[1:]
		rw = segsWidth(right)
	}
	avail := w - rw
	if rw > 0 {
		avail--
	}

	var b strings.Builder
	used := 0
	for _, s := range left {
		if used >= avail {
			break
		}
		text := s.text
		cut := false
		if used+ansi.StringWidth(text) > avail {
			text = truncate(text, avail-used)
			cut = true
		}
		used += ansi.StringWidth(text)
		if len(s.hl) == 0 {
			b.WriteString(paint(s.style, text))
			continue
		}
		n := len([]rune(text))
		if cut {
			n-- // keep the ellipsis plain
		}
		var idx []int
		for _, i := range s.hl {
			if i < n {
				idx = append(idx, i)
			}
		}
		hl, st := s.hlStyle, s.style
		if bg != nil {
			hl, st = hl.Background(bg), st.Background(bg)
		}
		b.WriteString(lipgloss.StyleRunes(text, idx, hl, st))
	}
	b.WriteString(paint(lipgloss.NewStyle(), strings.Repeat(" ", max(0, w-used-rw))))
	for _, s := range right {
		b.WriteString(paint(s.style, s.text))
	}
	return b.String()
}

// segsWidth is the width of segments in cells.
func segsWidth(segs []seg) int {
	n := 0
	for _, s := range segs {
		n += ansi.StringWidth(s.text)
	}
	return n
}

// fitBlock pads or cuts s to exactly w cells by h lines, so that panes join
// without overflow whatever their content.
func fitBlock(s string, w, h int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > h {
		lines = lines[:h]
	}
	for len(lines) < h {
		lines = append(lines, "")
	}
	for i, l := range lines {
		if lw := ansi.StringWidth(l); lw > w {
			lines[i] = ansi.Truncate(l, w, "")
		} else if lw < w {
			lines[i] = l + strings.Repeat(" ", w-lw)
		}
	}
	return strings.Join(lines, "\n")
}
