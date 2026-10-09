package cli

import (
	"io"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// Mid-tone colors read well on both light and dark backgrounds. The
// lipgloss writers downsample them, or strip them when output is not a
// terminal.
var (
	accent = lipgloss.Color("#8B7CF6")
	green  = lipgloss.Color("#3FB950")
	yellow = lipgloss.Color("#D29922")
	red    = lipgloss.Color("#F85149")
	muted  = lipgloss.Color("#8B949E")
)

func line(w io.Writer, mark lipgloss.Style, symbol, msg string) {
	lipgloss.Fprintln(w, "  "+mark.Render(symbol)+" "+msg)
}

func success(w io.Writer, msg string) { line(w, lipgloss.NewStyle().Foreground(green), "✓", msg) }
func notice(w io.Writer, msg string)  { line(w, lipgloss.NewStyle().Foreground(accent), "◆", msg) }
func warn(w io.Writer, msg string)    { line(w, lipgloss.NewStyle().Foreground(yellow), "!", msg) }

// table prints aligned rows; the first column is emphasized and the others
// dimmed.
func table(w io.Writer, rows [][]string) {
	widths := map[int]int{}
	for _, row := range rows {
		for i, cell := range row {
			widths[i] = max(widths[i], ansi.StringWidth(cell))
		}
	}
	first := lipgloss.NewStyle().Bold(true)
	rest := lipgloss.NewStyle().Foreground(muted)
	for _, row := range rows {
		var b strings.Builder
		b.WriteString("    ")
		for i, cell := range row {
			pad := ""
			if i < len(row)-1 {
				pad = strings.Repeat(" ", widths[i]-ansi.StringWidth(cell)+2)
			}
			if i == 0 {
				b.WriteString(first.Render(cell) + pad)
			} else {
				b.WriteString(rest.Render(cell) + pad)
			}
		}
		lipgloss.Fprintln(w, b.String())
	}
}

func heading(w io.Writer, title, sub string) {
	brand := lipgloss.NewStyle().Foreground(accent).Bold(true).Render(title)
	if sub != "" {
		brand += "  " + lipgloss.NewStyle().Foreground(muted).Render(sub)
	}
	lipgloss.Fprintln(w, "\n  "+brand+"\n")
}
