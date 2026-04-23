package tui

// panel_prompt.go — top slide-in showing the assembled system prompt that
// Selene will see on the next turn. Accent: moonlight-blue (Selene's voice),
// because this panel is literally "what Selene reads" rendered to the user.
//
// Observability has been the thing I've wanted most while building cairo —
// /prompt show is great from conversation but the TUI equivalent makes
// prompt composition ambient. Open the panel, see your context, press 'r'
// to refresh after a change.

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/reflow/wordwrap"

	"github.com/scotmcc/cairo/internal/agent"
)

const panelPromptID panelID = "prompt"

type promptState struct {
	viewport  viewport.Model
	content   string // raw assembled prompt
	wrapped   string // content after word-wrap at lastWidth
	lastWidth int    // width the wrap was computed against; re-wrap on change
	tokens    int    // rough token estimate — chars/4 heuristic
}

func init() {
	registerPanel(&panelSpec{
		ID:              panelPromptID,
		Position:        posTop,
		Accent:          colVoiceSelene,
		Title:           "prompt",
		Description:     "Show the assembled system prompt Selene will see on the next turn.",
		ToggleKey:       "ctrl+p",
		ShowInHelp:      true,
		PreferredHeight: 18,
		OnOpen:          promptOpen,
		OnClose:         promptClose,
		Update:          promptUpdate,
		View:            promptView,
	})
}

func promptOpen(m *model) tea.Cmd {
	// Size the viewport up front. Bubble Tea's value-receiver View means
	// mutations to the viewport during View() don't persist; dimensions
	// and content have to be set here (via the *model pointer) or from
	// Update. Re-set on WindowSizeMsg.
	vp := viewport.New(0, 0)
	m.prompt.viewport = vp
	promptResize(m)
	promptRefresh(m)
	return nil
}

// promptResize sets the viewport's width/height from current model state
// and re-wraps if width changed. Called on open and on WindowSizeMsg.
func promptResize(m *model) {
	w := m.width
	if w <= 0 {
		w = 80
	}
	// Panel-allocated space = PreferredHeight (18). Reserve 3 rows: title
	// + rule + hint. Viewport gets the remaining 15.
	h := 18 - 3
	m.prompt.viewport.Width = w
	m.prompt.viewport.Height = h
	if w != m.prompt.lastWidth && m.prompt.content != "" {
		promptRewrap(m)
	}
}

func promptClose(m *model) {
	m.prompt.content = ""
	m.prompt.tokens = 0
}

// promptRefresh rebuilds the system prompt from current session state and
// loads it into the viewport. Called on open, on 'r', and on a resize
// (to re-wrap at the new width).
func promptRefresh(m *model) {
	msg, err := agent.BuildSystemPrompt(m.db, m.session.ID, m.session.Role, m.session.CWD, nil)
	if err != nil {
		m.prompt.content = fmt.Sprintf("error building prompt: %v", err)
		m.prompt.tokens = 0
	} else {
		m.prompt.content = msg.Content
		// Rough chars/4 token estimate — fine for a glanceable "how big
		// is my prompt" signal. Not worth pulling in a real tokenizer.
		m.prompt.tokens = len(msg.Content) / 4
	}
	promptRewrap(m)
	m.prompt.viewport.GotoTop()
}

// promptRewrap word-wraps the current content to m.width and loads the
// wrapped text into the viewport. Split out from refresh so a resize can
// re-wrap without redoing the BuildSystemPrompt call.
func promptRewrap(m *model) {
	w := m.width
	if w <= 0 {
		w = 80 // sensible fallback before first WindowSizeMsg
	}
	m.prompt.wrapped = wordwrap.String(m.prompt.content, w)
	m.prompt.lastWidth = w
	m.prompt.viewport.SetContent(m.prompt.wrapped)
}

func promptUpdate(msg tea.Msg, m *model) (bool, tea.Cmd) {
	// Handle resize first — set the viewport's new dimensions and re-wrap
	// content to the new width so scroll offsets map to real wrapped rows.
	if _, ok := msg.(tea.WindowSizeMsg); ok {
		promptResize(m)
	}

	key, ok := msg.(tea.KeyMsg)
	if !ok {
		// Forward non-key messages to the viewport so mouse/scroll gestures
		// work. Don't claim — main Update handles other routing.
		newVp, cmd := m.prompt.viewport.Update(msg)
		m.prompt.viewport = newVp
		return false, cmd
	}
	switch key.String() {
	case "esc":
		m.closePanel(panelPromptID)
		return true, nil
	case "r":
		promptRefresh(m)
		return true, nil
	case "up", "down", "pgup", "pgdown", "home", "end":
		newVp, cmd := m.prompt.viewport.Update(msg)
		m.prompt.viewport = newVp
		return true, cmd
	}
	// Other keys: claim (while the panel is focused, arrows etc. shouldn't
	// leak to the main input field).
	return true, nil
}

func promptView(width, _ int, m *model) string {
	accent := lipgloss.NewStyle().Foreground(colVoiceSelene).Bold(true)
	dim := lipgloss.NewStyle().Foreground(colVoiceSelene2)

	// Note: viewport width/height and content are set via Update (resize)
	// and refresh, not here — mutations in View don't persist because the
	// outer View uses a value receiver. This function reads-and-renders
	// only.

	// Title shows token estimate and char count — a running sense of
	// prompt weight without having to go hunt for it.
	title := accent.Render("prompt") +
		dim.Render(fmt.Sprintf("  ·  ~%d tokens  ·  %d chars",
			m.prompt.tokens, len(m.prompt.content)))

	rule := m.styles.thinRule.Render(strings.Repeat("─", max(0, width)))
	body := m.prompt.viewport.View()
	hint := m.styles.statusHint.Render("  ↑↓ PgUp/PgDn scroll · r refresh · esc close")

	return title + "\n" + rule + "\n" + body + "\n" + hint
}
