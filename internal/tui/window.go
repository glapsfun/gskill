package tui

import "github.com/charmbracelet/lipgloss"

// Shared bounded-viewport helpers: one page computation and one more-marker
// windowing implementation for every scrolling list in the package (selector,
// wizard preview, wizard version step).

// pageFor returns the number of content rows that fit in a terminal of the
// given height with reserved frame rows, defaulting when the size is unknown
// and never dropping below one row.
func pageFor(height, reserved int) int {
	if height <= 0 {
		return defaultPageSize
	}
	if p := height - reserved; p > 0 {
		return p
	}
	return 1
}

// cursorOffset returns the window offset into n rows that keeps cursor
// visible on a page-sized viewport.
func cursorOffset(cursor, page, n int) int {
	offset := 0
	if cursor >= page {
		offset = cursor - page + 1
	}
	if maxOffset := n - page; offset > maxOffset {
		offset = maxOffset
	}
	if offset < 0 {
		offset = 0
	}
	return offset
}

// windowRows bounds rows to page lines starting at offset (clamped), framing
// the window with more-markers rendered in the given style.
func windowRows(rows []string, page, offset int, marker lipgloss.Style) []string {
	if len(rows) <= page {
		return rows
	}
	if maxOffset := len(rows) - page; offset > maxOffset {
		offset = maxOffset
	}
	if offset < 0 {
		offset = 0
	}

	out := make([]string, 0, page+2)
	if offset > 0 {
		out = append(out, marker.Render("  ↑ more"))
	}
	out = append(out, rows[offset:offset+page]...)
	if offset+page < len(rows) {
		out = append(out, marker.Render("  ↓ more"))
	}
	return out
}
