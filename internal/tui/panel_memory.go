package tui

// panel_memory.go — right-side memory spotlight. Type to semantic-search
// Selene's stored memories; arrow keys pick a result; Enter injects the
// selected memory into the main input as "[memory: ...]" so the user can
// reference it in their next turn.
//
// Panel accent: amber (colMemory) — the semantic color vocabulary says
// "amber means memory stuff," so every pixel of the spotlight reinforces
// that association.

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/scotmcc/cairo/internal/db"
)

const panelMemoryID panelID = "memory"

type memoryState struct {
	search      textinput.Model
	results     []*db.Memory
	selected    int
	lastQuery   string
	searchTimer time.Time // for debouncing search
}

func init() {
	registerPanel(&panelSpec{
		ID:             panelMemoryID,
		Position:       posRight,
		Accent:         colMemory,
		Title:          "memory",
		Description:    "Search Selene's memories. Enter inserts a reference into your next message.",
		ToggleKey:      "ctrl+m",
		ShowInHelp:     true,
		PreferredWidth: 42,
		OnOpen:         memoryOpen,
		OnClose:        memoryClose,
		Update:         memoryUpdate,
		View:           memoryView,
	})
}

func memoryOpen(m *model) tea.Cmd {
	ti := textinput.New()
	ti.Placeholder = "search memories…"
	ti.Prompt = ""
	ti.CharLimit = 0
	ti.PlaceholderStyle = lipgloss.NewStyle().Foreground(colTextDim)
	ti.TextStyle = lipgloss.NewStyle().Foreground(colText)
	ti.Focus()
	m.memory.search = ti
	m.memory.selected = 0
	m.memory.lastQuery = ""
	// Empty query: show most recent memories so the panel isn't blank on open.
	memoryRefresh(m, "")
	return textinput.Blink
}

func memoryClose(m *model) {
	m.memory.results = nil
	m.memory.selected = 0
}

// memoryRefresh updates the result list for the given query. Empty query
// returns recent memories; non-empty runs a semantic search via the agent's
// embedder. The embed call is synchronous here — for the small number of
// memories a single cairo DB typically holds this is fast enough; if the
// DB grows we'll want to move it to a tea.Cmd with debounce.
func memoryRefresh(m *model, query string) {
	m.memory.lastQuery = query
	query = strings.TrimSpace(query)

	if query == "" {
		all, err := m.db.Memories.All()
		if err != nil {
			m.memory.results = nil
			return
		}
		const limit = 50
		if len(all) > limit {
			all = all[:limit]
		}
		m.memory.results = all
		if m.memory.selected >= len(all) {
			m.memory.selected = 0
		}
		return
	}

	// Semantic search — try the embedder; fall back to substring match if
	// there's no embed model configured or the embed call fails.
	vec, err := m.agent.Embed(query)
	if err != nil || len(vec) == 0 {
		m.memory.results = memorySubstringSearch(m.db, query, 20)
	} else {
		results, err := m.db.Memories.Search(vec, 20)
		if err != nil || len(results) == 0 {
			m.memory.results = memorySubstringSearch(m.db, query, 20)
		} else {
			m.memory.results = results
		}
	}
	if m.memory.selected >= len(m.memory.results) {
		m.memory.selected = 0
	}
}

// memorySubstringSearch is the keyword-match fallback when semantic search
// isn't available. Case-insensitive substring over memory content.
func memorySubstringSearch(database *db.DB, query string, limit int) []*db.Memory {
	all, err := database.Memories.All()
	if err != nil {
		return nil
	}
	q := strings.ToLower(query)
	var out []*db.Memory
	for _, mem := range all {
		if strings.Contains(strings.ToLower(mem.Content), q) {
			out = append(out, mem)
			if len(out) >= limit {
				break
			}
		}
	}
	return out
}

func memoryUpdate(msg tea.Msg, m *model) (bool, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return false, nil
	}
	switch key.String() {
	case "esc":
		m.closePanel(panelMemoryID)
		return true, nil
	case "up":
		if m.memory.selected > 0 {
			m.memory.selected--
		}
		return true, nil
	case "down":
		if m.memory.selected < len(m.memory.results)-1 {
			m.memory.selected++
		}
		return true, nil
	case "enter":
		if len(m.memory.results) == 0 || m.memory.selected >= len(m.memory.results) {
			return true, nil
		}
		mem := m.memory.results[m.memory.selected]
		insertMemoryRef(m, mem)
		m.closePanel(panelMemoryID)
		return true, nil
	}
	// Any other key goes to the search input; after the input updates we
	// re-run the query and return the input's cmd (blink etc.).
	var cmd tea.Cmd
	m.memory.search, cmd = m.memory.search.Update(msg)
	q := m.memory.search.Value()
	if q != m.memory.lastQuery {
		memoryRefresh(m, q)
	}
	return true, cmd
}

func memoryView(width, height int, m *model) string {
	accent := lipgloss.NewStyle().Foreground(colMemory).Bold(true)
	hint := m.styles.statusHint
	dim := m.styles.statusMemLbl // amber, non-bold — goes with memory concept

	var b strings.Builder

	// Panel title row.
	b.WriteString(accent.Render("memory"))
	b.WriteByte('\n')
	b.WriteString(m.styles.thinRule.Render(strings.Repeat("─", max(0, width))))
	b.WriteByte('\n')

	// Search input row.
	b.WriteString(accent.Render("▸ "))
	b.WriteString(m.memory.search.View())
	b.WriteByte('\n')
	b.WriteString(m.styles.thinRule.Render(strings.Repeat("─", max(0, width))))
	b.WriteByte('\n')

	// Result list. Reserve rows: title(1) + rule(1) + search(1) + rule(1)
	// + footer hint(1) = 5 non-list rows.
	listHeight := max(1, height-5)

	if len(m.memory.results) == 0 {
		if m.memory.search.Value() == "" {
			b.WriteString(hint.Render("  (no memories yet)"))
		} else {
			b.WriteString(hint.Render("  no matches"))
		}
		b.WriteByte('\n')
		// Pad to maintain layout height
		for i := 0; i < listHeight-1; i++ {
			b.WriteByte('\n')
		}
		b.WriteString(hint.Render("  ↑↓ select · enter insert · esc close"))
		return b.String()
	}

	// Window around the selected index so it stays visible.
	start := 0
	if m.memory.selected >= listHeight {
		start = m.memory.selected - listHeight + 1
	}
	end := start + listHeight
	if end > len(m.memory.results) {
		end = len(m.memory.results)
	}

	for i := start; i < end; i++ {
		mem := m.memory.results[i]
		preview := oneLine(mem.Content, width-6) // leave room for id + space + selector
		id := fmt.Sprintf("[%d]", mem.ID)
		row := fmt.Sprintf("  %s %s", id, preview)
		if i == m.memory.selected {
			sel := lipgloss.NewStyle().
				Foreground(colFocus).
				Background(colSurfaceHi).
				Bold(true)
			b.WriteString(sel.Render(padRight(row, width)))
		} else {
			idStyled := dim.Render(id)
			contentStyled := m.styles.body.Render(preview)
			b.WriteString("  " + idStyled + " " + contentStyled)
		}
		b.WriteByte('\n')
	}

	// Pad remaining rows to keep layout height consistent.
	rendered := end - start
	for i := rendered; i < listHeight; i++ {
		b.WriteByte('\n')
	}

	b.WriteString(hint.Render("  ↑↓ select · enter insert · esc close"))
	return b.String()
}

// oneLine collapses multi-line memory content into a single line with a
// middle-ellipsis if it's longer than the panel width. Memories can be
// long; the panel just shows a preview to keep rows scannable.
func oneLine(s string, maxw int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\t", " ")
	// collapse multi-spaces
	for strings.Contains(s, "  ") {
		s = strings.ReplaceAll(s, "  ", " ")
	}
	if maxw < 10 {
		maxw = 10
	}
	if len(s) <= maxw {
		return s
	}
	// Truncate with trailing ellipsis — middle-ellipsis looks noisy in a
	// list of rows; tail ellipsis reads as "there's more."
	return s[:maxw-1] + "…"
}

// insertMemoryRef appends the selected memory's content to the user's input
// field, wrapped in brackets so Selene sees it as a user-provided reference
// block. The user can edit/remove the injection before submitting.
func insertMemoryRef(m *model, mem *db.Memory) {
	v := m.input.Value()
	ref := fmt.Sprintf("[memory: %s]", strings.TrimSpace(mem.Content))

	sep := " "
	if v == "" {
		sep = ""
	} else if strings.HasSuffix(v, " ") {
		sep = ""
	}
	m.input.SetValue(v + sep + ref + " ")
	// SetValue re-inserts from scratch, leaving cursor at end of content.
}
