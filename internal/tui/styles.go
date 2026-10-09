// Package tui is the full-screen terminal interface: the export dashboard and
// the configuration editor.
package tui

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// palette holds the interface colors for one background brightness.
type palette struct {
	accent, accentBg, text, muted, faint, border color.Color
	green, yellow, red                           color.Color
}

func newPalette(dark bool) palette {
	ld := lipgloss.LightDark(dark)
	return palette{
		accent:   ld(lipgloss.Color("#6D4AFF"), lipgloss.Color("#9D8CFF")),
		accentBg: ld(lipgloss.Color("#EDE9FE"), lipgloss.Color("#2A2342")),
		text:     ld(lipgloss.Color("#1F2328"), lipgloss.Color("#E6EDF3")),
		muted:    ld(lipgloss.Color("#656D76"), lipgloss.Color("#8B949E")),
		faint:    ld(lipgloss.Color("#AFB8C1"), lipgloss.Color("#484F58")),
		border:   ld(lipgloss.Color("#D0D7DE"), lipgloss.Color("#30363D")),
		green:    ld(lipgloss.Color("#1A7F37"), lipgloss.Color("#3FB950")),
		yellow:   ld(lipgloss.Color("#9A6700"), lipgloss.Color("#D29922")),
		red:      ld(lipgloss.Color("#CF222E"), lipgloss.Color("#F85149")),
	}
}

func fg(c color.Color) lipgloss.Style { return lipgloss.NewStyle().Foreground(c) }

// fit truncates or pads a styled string to exactly width cells.
func fit(s string, width int) string {
	if width <= 0 {
		return ""
	}
	s = ansi.Truncate(s, width, "…")
	if pad := width - ansi.StringWidth(s); pad > 0 {
		s += strings.Repeat(" ", pad)
	}
	return s
}

// panel draws a rounded box of exactly width × height cells with the title
// set into the top border. Lines beyond the inner height are dropped.
func panel(p palette, title string, lines []string, width, height int, focused bool) string {
	if width < 4 || height < 2 {
		return ""
	}
	borderColor := p.border
	titleStyle := fg(p.muted).Bold(true)
	if focused {
		borderColor = p.accent
		titleStyle = fg(p.accent).Bold(true)
	}
	b := fg(borderColor)
	inner := width - 4

	var out strings.Builder
	label := ""
	if title != "" {
		label = " " + titleStyle.Render(title) + " "
	}
	fill := width - 3 - ansi.StringWidth(label)
	if fill < 0 {
		label, fill = "", width-3
	}
	out.WriteString(b.Render("╭─") + label + b.Render(strings.Repeat("─", fill)+"╮") + "\n")

	for i := 0; i < height-2; i++ {
		line := ""
		if i < len(lines) {
			line = lines[i]
		}
		out.WriteString(b.Render("│") + " " + fit(line, inner) + " " + b.Render("│") + "\n")
	}
	out.WriteString(b.Render("╰" + strings.Repeat("─", width-2) + "╯"))
	return out.String()
}

// bar draws a progress bar of width cells.
func bar(p palette, ratio float64, width int) string {
	if width <= 0 {
		return ""
	}
	ratio = max(0, min(1, ratio))
	filled := int(ratio * float64(width))
	return fg(p.accent).Render(strings.Repeat("━", filled)) + fg(p.faint).Render(strings.Repeat("─", width-filled))
}
