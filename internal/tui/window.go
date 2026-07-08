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

// windowBounds clamps a scroll offset for n rows on a page-sized viewport and
// reports whether more-markers are needed above and below.
func windowBounds(n, page, offset int) (start, end int, above, below bool) {
	if n <= page {
		return 0, n, false, false
	}
	if maxOffset := n - page; offset > maxOffset {
		offset = maxOffset
	}
	if offset < 0 {
		offset = 0
	}
	return offset, offset + page, offset > 0, offset+page < n
}

// windowRows bounds rows to page lines starting at offset (clamped), framing
// the window with more-markers rendered in the given style.
func windowRows(rows []string, page, offset int, marker lipgloss.Style) []string {
	start, end, above, below := windowBounds(len(rows), page, offset)
	if !above && !below {
		return rows
	}
	out := make([]string, 0, page+2)
	if above {
		out = append(out, marker.Render("  ↑ more"))
	}
	out = append(out, rows[start:end]...)
	if below {
		out = append(out, marker.Render("  ↓ more"))
	}
	return out
}
