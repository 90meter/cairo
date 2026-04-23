package tui

// panel_threads.go — left drawer listing running and recent background
// tasks. Each task is one of Selene's parallel threads of attention.
// Tinted lavender (the threads concept color) to reinforce the semantic
// color vocabulary.

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/scotmcc/cairo/internal/db"
)

const panelThreadsID panelID = "threads"

// threadsState is the panel's per-model state. Lives on model.threads so
// the panel spec can read/write via hook closures.
type threadsState struct {
	tasks    []*db.Task
	selected int
}

func init() {
	registerPanel(&panelSpec{
		ID:             panelThreadsID,
		Position:       posLeft,
		Accent:         colThread,
		Title:          "threads",
		Description:    "Running and recent background tasks — Selene's parallel threads of attention.",
		ToggleKey:      "ctrl+t",
		ShowInHelp:     true,
		PreferredWidth: 36,
		OnOpen:         threadsRefresh,
		Update:         threadsUpdate,
		View:           threadsView,
	})
}

// threadsRefresh loads the most recent tasks from the DB. Called when the
// panel opens and when the user presses 'r' to refresh. Later we can make
// it live by subscribing to task status changes. Returns nil — pure sync.
func threadsRefresh(m *model) tea.Cmd {
	tasks, err := m.db.Tasks.RecentAll(30)
	if err != nil {
		m.threads.tasks = nil
		return nil
	}
	m.threads.tasks = tasks
	if m.threads.selected >= len(tasks) {
		m.threads.selected = 0
	}
	return nil
}

func threadsUpdate(msg tea.Msg, m *model) (bool, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return false, nil
	}
	switch key.String() {
	case "up":
		if m.threads.selected > 0 {
			m.threads.selected--
		}
		return true, nil
	case "down":
		if m.threads.selected < len(m.threads.tasks)-1 {
			m.threads.selected++
		}
		return true, nil
	case "r":
		threadsRefresh(m)
		return true, nil
	case "esc":
		m.closePanel(panelThreadsID)
		return true, nil
	}
	return false, nil
}

func threadsView(width, height int, m *model) string {
	// Reserve: title(1) + rule(1) + footer hint(1) + surrounding blanks(1).
	listHeight := max(1, height-4)

	var b strings.Builder

	// Title with accent color (lavender for threads).
	title := lipgloss.NewStyle().
		Foreground(colThread).Bold(true).
		Render("threads")
	b.WriteString(title)
	b.WriteByte('\n')
	b.WriteString(m.styles.thinRule.Render(strings.Repeat("─", max(0, width))))
	b.WriteByte('\n')

	if len(m.threads.tasks) == 0 {
		b.WriteString(m.styles.statusHint.Render("  (no tasks yet)\n"))
		b.WriteString(m.styles.statusHint.Render("\n  r  refresh\n  esc  close"))
		return b.String()
	}

	// Window the list around the selected index so it's always visible.
	start := 0
	if m.threads.selected >= listHeight {
		start = m.threads.selected - listHeight + 1
	}
	end := start + listHeight
	if end > len(m.threads.tasks) {
		end = len(m.threads.tasks)
	}

	for i := start; i < end; i++ {
		task := m.threads.tasks[i]
		row := formatThreadRow(task, width)
		if i == m.threads.selected {
			sel := lipgloss.NewStyle().
				Foreground(colFocus).
				Background(colSurfaceHi).
				Bold(true)
			b.WriteString(sel.Render(padRight(row, width)))
		} else {
			b.WriteString(colorizeThreadRow(task, width))
		}
		b.WriteByte('\n')
	}

	b.WriteString(m.styles.statusHint.Render("\n  ↑↓ navigate · r refresh · esc close"))
	return b.String()
}

// formatThreadRow builds the plain text for a task row. colorizeThreadRow
// does the styled version. We split them so the selected-row highlighter
// can overlay its own style on a predictable layout.
func formatThreadRow(t *db.Task, width int) string {
	glyph := statusGlyph(t.Status)
	age := humanAge(t.CreatedAt)
	title := t.Title
	maxTitle := width - 16 // glyph(1) + space + role prefix + age + padding
	if maxTitle < 10 {
		maxTitle = 10
	}
	if len(title) > maxTitle {
		title = title[:maxTitle-1] + "…"
	}
	return fmt.Sprintf("  %s %-*s  %s", glyph, maxTitle, title, age)
}

func colorizeThreadRow(t *db.Task, width int) string {
	glyph := statusGlyph(t.Status)
	age := humanAge(t.CreatedAt)
	title := t.Title
	maxTitle := width - 16
	if maxTitle < 10 {
		maxTitle = 10
	}
	if len(title) > maxTitle {
		title = title[:maxTitle-1] + "…"
	}

	glyphStyle := lipgloss.NewStyle().Foreground(statusColor(t.Status)).Bold(true)
	titleStyle := lipgloss.NewStyle().Foreground(colText)
	ageStyle := lipgloss.NewStyle().Foreground(colTextDim)

	return fmt.Sprintf("  %s %s  %s",
		glyphStyle.Render(glyph),
		titleStyle.Render(padRight(title, maxTitle)),
		ageStyle.Render(age))
}

// statusGlyph returns a compact marker for each task status.
//
//	◇  running (pulsing, conceptually — v3 polish)
//	◆  done
//	✗  failed
//	○  pending / blocked
func statusGlyph(status string) string {
	switch status {
	case "running":
		return "◇"
	case "done":
		return "◆"
	case "failed":
		return "✗"
	default:
		return "○"
	}
}

// statusColor picks a color per status. Running tasks use the threads
// lavender to reinforce the concept; done uses ok green; failed uses err.
func statusColor(status string) lipgloss.Color {
	switch status {
	case "running":
		return colThread
	case "done":
		return colOK
	case "failed":
		return colErr
	default:
		return colTextDim
	}
}

// humanAge renders a compact "Xs" / "Xm" / "Xh" / "Xd" suffix.
func humanAge(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}
