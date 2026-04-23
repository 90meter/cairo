package tui

// panels.go — the drawer/overlay system. Every panel the TUI can show
// (threads on the left, memory spotlight on the right, soul editor on top,
// fullscreen help) registers a panelSpec here; the main model tracks which
// panels are currently open and which one has keyboard focus.
//
// Design rules — the same ones that drive the rest of the TUI:
//   - Panels are UI state, not DB state. Opening/closing a panel never
//     writes to Selene's mind.
//   - Exactly one panel can be keyboard-focused at a time. The input field
//     is focused when no panel is. Opening a panel shifts focus to it;
//     closing returns focus to the last-focused panel or input.
//   - At most one panel per position. Opening a same-position panel closes
//     the prior occupant.
//   - Fullscreen panels hide the transcript but keep header and status-bar
//     visible so the user's location in the program is always legible.

import (
	"sort"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// panelID identifies a registered panel. Use typed IDs so toggle keys,
// registry lookups, and focus tracking stay consistent across the codebase.
type panelID string

// panelPosition is where a panel renders in the screen layout.
type panelPosition int

const (
	// posTop renders between the header rule and the transcript.
	posTop panelPosition = iota
	// posLeft splits the transcript horizontally: panel | transcript.
	posLeft
	// posRight splits the transcript horizontally: transcript | panel.
	posRight
	// posBottom renders between the transcript and the input frame.
	posBottom
	// posFullscreen replaces the transcript entirely (header + status stay).
	posFullscreen
)

// panelSpec declares a panel: how it renders, how it handles input, what
// toggles it, what colors it wears. Every concrete panel file registers one
// of these in its init().
type panelSpec struct {
	ID          panelID
	Position    panelPosition
	Accent      lipgloss.Color // used for title, border highlights
	Title       string         // one-word panel name
	Description string         // shown in help overlay
	// ToggleKey is the tea.Key string that opens/closes this panel. "" means
	// the panel is opened programmatically (e.g. slash drawer via "/").
	ToggleKey string
	// ShowInHelp controls whether this panel appears in the help overlay.
	// Slash commands and transient panels may want to stay out.
	ShowInHelp bool

	// Lifecycle hooks. Called with the model pointer so they can read/mutate
	// panel state stored on m (kept there rather than inside the spec so
	// hot reload / re-registration stays painless). OnOpen may return a
	// tea.Cmd — useful for components like filepicker whose Init() kicks
	// off a directory-read task.
	OnOpen  func(*model) tea.Cmd
	OnClose func(*model)

	// Per-frame callbacks. Update receives the incoming tea.Msg when this
	// panel is focused; return (true, cmd) to claim the message, (false,
	// nil) to let it fall through to the next handler (input, typically).
	Update func(msg tea.Msg, m *model) (handled bool, cmd tea.Cmd)
	View   func(width, height int, m *model) string

	// Size preferences. 0 picks a sensible default. Left/right panels use
	// PreferredWidth; top/bottom use PreferredHeight.
	PreferredWidth  int
	PreferredHeight int
}

// --- registry ---

var registeredPanels []*panelSpec

// registerPanel adds a spec to the global registry. Call from init().
// Panics on duplicate ID — panels are a small finite set and collisions
// mean a code bug, not runtime data.
func registerPanel(s *panelSpec) {
	for _, existing := range registeredPanels {
		if existing.ID == s.ID {
			panic("tui: duplicate panel ID " + string(s.ID))
		}
	}
	registeredPanels = append(registeredPanels, s)
}

func findPanel(id panelID) *panelSpec {
	for _, s := range registeredPanels {
		if s.ID == id {
			return s
		}
	}
	return nil
}

// panelByToggleKey returns the panel whose ToggleKey matches the given tea
// key string, or nil if no panel is bound to that key.
func panelByToggleKey(key string) *panelSpec {
	if key == "" {
		return nil
	}
	for _, s := range registeredPanels {
		if s.ToggleKey == key {
			return s
		}
	}
	return nil
}

// helpablePanels returns panels that should appear in the help overlay,
// sorted alphabetically by title for a stable reading order.
func helpablePanels() []*panelSpec {
	var out []*panelSpec
	for _, s := range registeredPanels {
		if s.ShowInHelp {
			out = append(out, s)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Title < out[j].Title })
	return out
}

// --- model open/close helpers ---

// openPanel makes the panel visible and gives it keyboard focus. If another
// panel already occupies the same position, that panel is closed first.
// Returns any tea.Cmd from OnOpen so callers can chain it into the program's
// command stream.
func (m *model) openPanel(id panelID) tea.Cmd {
	spec := findPanel(id)
	if spec == nil {
		return nil
	}
	if m.openPanels == nil {
		m.openPanels = make(map[panelID]bool)
	}
	// Close any other panel at the same position (except fullscreen — those
	// stack with others below them transparently for the panel registry
	// even though visually they cover everything).
	if spec.Position != posFullscreen {
		for openID := range m.openPanels {
			if openID == id {
				continue
			}
			other := findPanel(openID)
			if other != nil && other.Position == spec.Position {
				m.closePanel(openID)
			}
		}
	}
	wasOpen := m.openPanels[id]
	m.openPanels[id] = true
	m.focusedPanel = id
	var cmd tea.Cmd
	if !wasOpen && spec.OnOpen != nil {
		cmd = spec.OnOpen(m)
	}
	m.relayout()
	return cmd
}

// closePanel hides the panel. If it was focused, focus migrates to another
// open panel (any one — we don't track a focus stack yet) or to the input.
func (m *model) closePanel(id panelID) {
	if !m.openPanels[id] {
		return
	}
	delete(m.openPanels, id)
	if spec := findPanel(id); spec != nil && spec.OnClose != nil {
		spec.OnClose(m)
	}
	if m.focusedPanel == id {
		m.focusedPanel = ""
		for other := range m.openPanels {
			m.focusedPanel = other
			break
		}
	}
	m.relayout()
}

// togglePanel opens if closed, closes if open. Returns any tea.Cmd from
// the open path — nil when toggling to closed.
func (m *model) togglePanel(id panelID) tea.Cmd {
	if m.openPanels[id] {
		m.closePanel(id)
		return nil
	}
	return m.openPanel(id)
}

// isPanelOpen reports whether a given panel ID is currently visible.
func (m *model) isPanelOpen(id panelID) bool {
	return m.openPanels[id]
}

// panelsAt returns all open panels at the given position, in registry order.
func (m *model) panelsAt(pos panelPosition) []*panelSpec {
	var out []*panelSpec
	for _, s := range registeredPanels {
		if m.openPanels[s.ID] && s.Position == pos {
			out = append(out, s)
		}
	}
	return out
}

// focusedPanelSpec returns the spec for the currently-focused panel, or nil
// if the input is focused.
func (m *model) focusedPanelSpec() *panelSpec {
	if m.focusedPanel == "" {
		return nil
	}
	return findPanel(m.focusedPanel)
}
