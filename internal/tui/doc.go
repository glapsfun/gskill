// Package tui hosts the Bubble Tea dashboard, SKILL.md preview, and the
// interactive multi-select skill picker, rendering over the app service layer
// and refusing to launch without a TTY. The picker presents discovered skills
// through a bounded, scrolling viewport with a substring filter, so it stays
// usable for sources that discover many skills.
package tui
