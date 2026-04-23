package tui

// tui.go — Bubble Tea model, update, view. v1: conversation viewport,
// single-line input, status bar footer, role-tinted prompt glyph, streaming
// tokens from the agent event bus. Overlays and drawers come in v2.

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/scotmcc/cairo/internal/agent"
	"github.com/scotmcc/cairo/internal/db"
)

// Run starts the Bubble Tea program. Blocks until the user quits. Drains the
// agent's background goroutines (summarizer, etc.) on the way out.
func Run(a *agent.Agent, database *db.DB, session *db.Session) error {
	// Pin color profile so lipgloss doesn't need to probe the terminal.
	lipgloss.DefaultRenderer().SetColorProfile(termenv.TrueColor)

	m := newModel(a, database, session)
	p := tea.NewProgram(m,
		tea.WithAltScreen(),
		// Even with the profile pinned, bubbletea's package-init fires
		// lipgloss.HasDarkBackground() which sends an OSC 11 ("query
		// background color") to the terminal. Emulators that respond slowly
		// (Waveterm has been observed to take longer than the probe's 5s
		// timeout) end up replying while our input reader is already live,
		// so the raw escape — "]11;rgb:0000/0000/0000\" and friends — gets
		// injected into the textinput as if the user typed it. Bubbletea
		// parses OSC sequences as alt+']' followed by plain runes (it only
		// knows CSI responses), so we filter them explicitly here.
		tea.WithFilter(oscFilter()),
	)
	_, err := p.Run()
	// Drain whatever the agent has running in the background before we return.
	a.Close()
	return err
}

// oscBodyRe matches the characters that appear inside an OSC 10/11 response
// body — digit/hex triples like "rgb:0000/0000/0000", the OSC number itself,
// separators. Conservative by design: we'd rather let a real keypress
// through than swallow one.
var oscBodyRe = regexp.MustCompile(`^[0-9a-fA-F;:/rgbRGB()\\\s,.]+$`)

// oscFilter returns a message filter that drops OSC 10/11 responses that
// leak into the input stream when the terminal emulator replies late. It
// keeps state across invocations via closure.
//
// Shape of what we see after the terminal responds with ESC ] 11 ; rgb:... :
//   1. a KeyMsg with Alt=true and Runes=[']'] — ESC followed by ']' —
//      bubbletea interprets ESC as the Alt modifier on the next rune.
//   2. a KeyMsg with Type=KeyRunes containing the rest: "11;rgb:0000/0000/0000"
//   3. sometimes a trailing backslash from a ST terminator rendering.
//
// We enter osc-sink mode on (1) and stay there for exactly one more message
// that looks like a body (2). Anything that doesn't match ends sink mode and
// passes through — we bias toward letting real keys through if we're unsure.
func oscFilter() func(tea.Model, tea.Msg) tea.Msg {
	inOSC := false
	return func(_ tea.Model, msg tea.Msg) tea.Msg {
		key, ok := msg.(tea.KeyMsg)
		if !ok {
			return msg
		}
		if !inOSC {
			// Detect start: alt-modified ']' is how bubbletea presents ESC ].
			if key.Alt && len(key.Runes) == 1 && key.Runes[0] == ']' {
				inOSC = true
				return nil
			}
			return msg
		}
		// We're in OSC-sink mode. Expect a runs-of-text message containing
		// the response body. Drop it if it looks OSC-ish; otherwise let the
		// key through (we over-matched the start).
		inOSC = false
		if key.Type == tea.KeyRunes {
			if oscBodyRe.MatchString(string(key.Runes)) {
				return nil
			}
		}
		if key.Type == tea.KeyEscape {
			// ST terminator (ESC \) arriving as plain escape.
			return nil
		}
		return msg
	}
}

// --- model ---

type model struct {
	agent   *agent.Agent
	db      *db.DB
	session *db.Session

	// Layout
	width, height int
	ready         bool

	// UI components. input is a textarea so long messages wrap across
	// multiple lines instead of scrolling horizontally off-screen. Enter
	// submits; newlines are inserted via Alt-Enter / Shift-Enter (see the
	// key handler in Update).
	viewport viewport.Model
	input    textarea.Model

	// Transcript buffer — append-only, rendered into the viewport on each
	// update. Streaming tokens land here as they arrive. Pointer so the
	// model can be passed by value (Bubble Tea idiom for Update) without
	// triggering strings.Builder's no-copy-after-write check.
	transcript *strings.Builder

	// Streaming state — whether Selene is mid-turn, plus the cancel handle
	// for the in-flight context so Ctrl-C can abort the turn without killing
	// the program.
	streaming bool
	cancel    context.CancelFunc

	// Identity / status
	aiName      string
	memoryCount int
	threadCount int // running background tasks, refreshed on each tick

	// tickCounter increments on every tickMsg — drives subtle animations
	// (thread spinner in the status bar, breathing "thinking" indicator
	// when Selene is streaming). Wrapping at a reasonable modulus so we
	// don't drift into huge numbers over long sessions.
	tickCounter int

	// Commands + drawers. The slash drawer opens only when the user types
	// '/' as the first char of empty input; closes when the leading '/' is
	// deleted or Esc is pressed.
	commands     []Command
	slashOpen    bool
	slashMatches []Command
	slashIndex   int // selected row in the drawer

	// Panel system — every overlay/drawer other than the slash drawer
	// routes through panelSpec registration. openPanels tracks which are
	// visible; focusedPanel gets keyboard input first (input field is
	// focused when focusedPanel == "").
	openPanels   map[panelID]bool
	focusedPanel panelID

	// Per-panel state. Kept on the main model so panel hooks can access
	// without passing state values around.
	threads  threadsState
	files    filesState
	memory   memoryState
	prompt   promptState
	sessions sessionsState

	// Event subscription — a channel that carries agent events. The Bubble
	// Tea program polls it via listenEvents() which returns a tea.Cmd.
	eventCh <-chan agent.Event
	unsub   func()

	styles styles
}

func newModel(a *agent.Agent, database *db.DB, sess *db.Session) model {
	aiName, _ := database.Config.Get("ai_name")
	if aiName == "" {
		aiName = "cairo"
	}

	ti := textarea.New()
	ti.Placeholder = "message " + aiName + "…"
	ti.CharLimit = 0 // no limit
	ti.Prompt = ""   // we draw our own role-tinted glyph in View
	ti.ShowLineNumbers = false
	// Fixed 3-row input area. Long messages wrap within the box and scroll
	// internally as you keep typing — simpler than auto-growing which would
	// need us to re-measure visual wrap width on every keystroke. Three rows
	// is enough to see a couple of lines of what you're composing.
	ti.SetHeight(3)
	ti.MaxHeight = 3
	// Colors: placeholder dim but legible; typed text in primary color.
	ti.FocusedStyle.Placeholder = lipgloss.NewStyle().Foreground(colTextDim)
	ti.BlurredStyle.Placeholder = lipgloss.NewStyle().Foreground(colTextDim)
	ti.FocusedStyle.Text = lipgloss.NewStyle().Foreground(colText)
	ti.BlurredStyle.Text = lipgloss.NewStyle().Foreground(colText)
	// Hide the built-in cursor-line background highlight — it competes
	// with the prompt glyph and makes the box look busy at rest.
	ti.FocusedStyle.CursorLine = lipgloss.NewStyle()
	// Rebind "insert newline" so it doesn't eat plain Enter — our Update
	// intercepts Enter to submit the message. Alt-Enter (and Ctrl-J as a
	// terminal-friendly fallback) are for users who want deliberate
	// line breaks within a message.
	ti.KeyMap.InsertNewline = key.NewBinding(key.WithKeys("alt+enter", "ctrl+j"))
	ti.Focus()

	ch, unsub := a.Bus().Subscribe()

	m := model{
		agent:      a,
		db:         database,
		session:    sess,
		input:      ti,
		aiName:     aiName,
		commands:   defaultCommands(),
		eventCh:    ch,
		unsub:      unsub,
		styles:     newStyles(sess.Role),
		transcript: &strings.Builder{},
	}
	m.refreshCounts()
	return m
}

func (m *model) refreshCounts() {
	if n, err := m.db.Memories.Count(); err == nil {
		m.memoryCount = n
	}
	// Live thread count — drives the ◇ N marker and its pulse animation.
	if n, err := m.db.Tasks.CountRunning(); err == nil {
		m.threadCount = n
	}
}

// --- init ---

// tickInterval governs the animation cadence: thread spinner, breathing
// streaming indicator, and live status-bar refresh. 300ms is slow enough
// to be calm (not a flicker) but fast enough to feel like presence.
const tickInterval = 300 * time.Millisecond

// tickMsg fires every tickInterval via a self-rescheduling tea.Cmd.
// Carries no payload — the model holds its own counter.
type tickMsg struct{}

// scheduleTick returns a tea.Cmd that produces a tickMsg after tickInterval.
// The Update handler re-issues it on each tick, keeping the pulse going.
func scheduleTick() tea.Cmd {
	return tea.Tick(tickInterval, func(time.Time) tea.Msg { return tickMsg{} })
}

func (m model) Init() tea.Cmd {
	return tea.Batch(
		listenEvents(m.eventCh),
		textinput.Blink,
		scheduleTick(),
	)
}

// --- update ---

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.relayout()

	case tea.KeyMsg:
		key := msg.String()

		// Focused panel gets first crack at the key. If it claims the
		// message (returns handled=true), we stop. This is how panels
		// own up/down, enter, esc for their own navigation without the
		// main Update needing to know what each one does.
		if spec := m.focusedPanelSpec(); spec != nil && spec.Update != nil {
			if handled, cmd := spec.Update(msg, &m); handled {
				return m, cmd
			}
		}

		// Panel toggle keys — opening/closing via bound keys. Runs before
		// slash-drawer and input-field handling so Ctrl-T can't get captured
		// as input. Ctrl-modified keys always toggle (they can't be typed as
		// text anyway); plain keys (like "?") only toggle when the input is
		// empty, so you can type "?" mid-message without surprise.
		if spec := panelByToggleKey(key); spec != nil {
			ctrlKey := strings.HasPrefix(key, "ctrl+")
			if ctrlKey || m.input.Value() == "" {
				return m, m.togglePanel(spec.ID)
			}
		}

		// Slash drawer active: drawer-specific navigation + Esc handling.
		if m.slashOpen {
			switch key {
			case "esc", "ctrl+c":
				// Ctrl-C is bound globally to /clear, but while the slash
				// drawer is open we want it to just close the drawer (like
				// Esc) — wiping the transcript mid-command-selection
				// would be jarring.
				m.input.SetValue("")
				m.closeSlash()
				return m, nil
			case "up":
				if m.slashIndex > 0 {
					m.slashIndex--
				}
				return m, nil
			case "down":
				if m.slashIndex < len(m.slashMatches)-1 {
					m.slashIndex++
				}
				return m, nil
			case "enter":
				// Execute the selected command.
				if len(m.slashMatches) > 0 && m.slashIndex < len(m.slashMatches) {
					cmd := m.slashMatches[m.slashIndex]
					m.input.SetValue("")
					m.closeSlash()
					handlerCmd := cmd.Handler(&m)
					return m, handlerCmd
				}
				return m, nil
			}
			// Any other key: let the input handle it, then refresh the
			// filter — if the leading '/' got erased, close the drawer.
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			cmds = append(cmds, cmd)
			m.refreshSlash()
			return m, tea.Batch(cmds...)
		}

		// Ctrl-C is context-sensitive — its meaning depends on whether
		// Selene is mid-turn, whether the input has content, and otherwise
		// falls through to clearing the transcript view. Matches the
		// intuition that Ctrl-C means "stop whatever I was doing" and the
		// "doing" part shifts with state. All three forms are non-
		// destructive relative to Selene's DB — see design rule above.
		if key == "ctrl+c" {
			switch {
			case m.streaming && m.cancel != nil:
				// Abort the in-flight turn. runLoop catches ctx.Err(),
				// persists partial text with an (interrupted) tag, and
				// fires EventTurnEnd — the UI state resets naturally.
				m.cancel()
				m.cancel = nil
				return m, nil
			case m.input.Value() != "":
				m.input.SetValue("")
				return m, nil
			default:
				// Idle and empty: clear the transcript view. DB untouched.
				m.transcript.Reset()
				m.pushViewport()
				return m, nil
			}
		}

		// Global hotkeys (registry-bound). Checked before text input so
		// binding a key doesn't leak a character into the field.
		if bound := lookupByHotkey(m.commands, key); bound != nil {
			return m, bound.Handler(&m)
		}

		// Ctrl-D on empty input: Unix EOF, quits.
		switch key {
		case "ctrl+d":
			if m.input.Value() == "" {
				return m, tea.Quit
			}
			// Non-empty: treat as forward-delete via the text input.
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			return m, cmd
		case "/":
			// First-char slash: open the drawer. Otherwise a literal char.
			if m.input.Value() == "" {
				m.openSlash()
				// Fall through so the '/' also gets typed into the input,
				// keeping the drawer's filter query aligned with what the
				// user sees.
			}
		case "@":
			// '@' at a word boundary opens the file picker so the user can
			// browse instead of typing the path. The literal '@' still gets
			// typed into the input — insertFileRef looks for a trailing '@'
			// on selection and inserts the path right after it, so the user
			// experiences it as autocomplete of what they just started.
			v := m.input.Value()
			atBoundary := v == "" || strings.HasSuffix(v, " ") || strings.HasSuffix(v, "\t")
			if atBoundary {
				// openPanel may return a cmd (filepicker.Init); batch it
				// alongside the normal input handling that follows.
				if cmd := m.openPanel(panelFilesID); cmd != nil {
					cmds = append(cmds, cmd)
				}
			}
		case "enter":
			raw := strings.TrimSpace(m.input.Value())
			if raw == "" || m.streaming {
				break
			}
			m.input.SetValue("")
			// Prefix expansion: !shell runs a command and uses the output
			// as the user turn. @file (added separately) injects file
			// contents into what Selene sees without cluttering the
			// transcript with the full file body.
			displayed, sent := m.expandPrefixes(raw)
			m.appendUser(displayed)
			m.startAssistant()
			cmds = append(cmds, m.submit(sent))
			return m, tea.Batch(cmds...)
		case "pgup", "pgdown", "up", "down":
			if m.input.Value() == "" {
				var cmd tea.Cmd
				m.viewport, cmd = m.viewport.Update(msg)
				cmds = append(cmds, cmd)
				return m, tea.Batch(cmds...)
			}
		}

		// Default: pass to the text input.
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		cmds = append(cmds, cmd)
		return m, tea.Batch(cmds...)

	case eventMsg:
		m.handleEvent(msg.event)
		// Re-issue the listen command to keep the event pump going.
		cmds = append(cmds, listenEvents(m.eventCh))

	case promptErrMsg:
		m.appendSystem(fmt.Sprintf("error: %v", msg.err))
		m.streaming = false
		m.cancel = nil

	case turnCompleteMsg:
		m.streaming = false
		m.cancel = nil
		m.refreshCounts()

	case tickMsg:
		// Animation heartbeat. Cheap work — increment the counter and
		// refresh ambient stats (memory + thread counts) so the status
		// bar is always fresh, then schedule the next tick.
		m.tickCounter++
		m.refreshCounts()
		cmds = append(cmds, scheduleTick())
	}

	// Non-key messages: route to the focused panel first (this is how
	// filepicker's async readDirMsg reaches it, how any future panel's
	// tick/timer messages land, etc.), then always pass to the viewport
	// so mouse/scroll gestures still work.
	if _, isKey := msg.(tea.KeyMsg); !isKey {
		if spec := m.focusedPanelSpec(); spec != nil && spec.Update != nil {
			if _, cmd := spec.Update(msg, &m); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
		var vpCmd tea.Cmd
		m.viewport, vpCmd = m.viewport.Update(msg)
		cmds = append(cmds, vpCmd)
	}

	return m, tea.Batch(cmds...)
}

// openSlash puts the model into slash-drawer mode. Called when the user
// types '/' as the first char of an otherwise-empty input.
func (m *model) openSlash() {
	m.slashOpen = true
	m.slashIndex = 0
	m.slashMatches = filterCommands(m.commands, "")
}

// closeSlash tears down drawer state.
func (m *model) closeSlash() {
	m.slashOpen = false
	m.slashMatches = nil
	m.slashIndex = 0
}

// refreshSlash re-filters the drawer based on the current input. If the
// leading '/' has been erased (input no longer starts with /), closes.
func (m *model) refreshSlash() {
	v := m.input.Value()
	if !strings.HasPrefix(v, "/") {
		m.closeSlash()
		return
	}
	query := strings.TrimPrefix(v, "/")
	m.slashMatches = filterCommands(m.commands, query)
	if m.slashIndex >= len(m.slashMatches) {
		m.slashIndex = 0
	}
}

// relayout recomputes the viewport height based on the current window size
// and which overlays are open. Called from WindowSizeMsg and whenever a
// drawer/panel opens or closes.
func (m *model) relayout() {
	// Reserve rows (non-viewport, always present):
	//   title(1) + heavy-rule(1) + thin-top(1) + input(3) + thin-bot(1) + status(1) = 8
	// input uses 3 rows so messages that wrap stay legible while they're
	// being composed.
	reserved := 8
	if m.slashOpen {
		// Drawer eats up to drawerHeight rows between transcript and input.
		reserved += drawerHeight(m)
	}
	// Open top/bottom panels consume vertical space too. Without this the
	// viewport thought it still had the full terminal height and the total
	// output would overflow the terminal — the top of the screen (header
	// + panel title) would scroll off and vanish. Left/right panels carve
	// horizontal space, handled separately in renderTranscriptWithSides.
	for _, s := range registeredPanels {
		if !m.openPanels[s.ID] {
			continue
		}
		if s.Position == posTop || s.Position == posBottom {
			h := s.PreferredHeight
			if h == 0 {
				h = 8
			}
			reserved += h
		}
	}
	vpHeight := max(m.height-reserved, 3)
	if !m.ready {
		m.viewport = viewport.New(m.width, vpHeight)
		m.ready = true
	} else {
		m.viewport.Width = m.width
		m.viewport.Height = vpHeight
	}
	m.viewport.SetContent(m.transcript.String())
	m.viewport.GotoBottom()
	// Leave room for the role-tinted glyph ("▸ " = 2 cells) + a small
	// right margin so long lines don't kiss the terminal edge.
	m.input.SetWidth(max(10, m.width-4))
}

// drawerHeight computes how many rows the slash drawer should consume —
// proportional to the number of matches, clamped to a sensible range.
func drawerHeight(m *model) int {
	h := len(m.slashMatches)
	if h == 0 {
		h = 1
	}
	// +1 for a thin rule above and a short footer hint row
	return min(h+2, 10)
}

// submit sends a prompt to the agent on a background goroutine. The actual
// streaming / tool events arrive via the event bus. Each submission gets a
// fresh cancel handle; Ctrl-C while streaming calls it to abort the turn.
func (m *model) submit(text string) tea.Cmd {
	a := m.agent
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	return func() tea.Msg {
		if err := a.Prompt(ctx, text); err != nil {
			return promptErrMsg{err: err}
		}
		return turnCompleteMsg{}
	}
}

// --- view ---

func (m model) View() string {
	if !m.ready {
		return "initializing…"
	}

	// Fullscreen panel (help) replaces transcript + side panels; header and
	// status-bar still render so the user's location in the program stays
	// legible even while reading a modal.
	if fs := m.panelsAt(posFullscreen); len(fs) > 0 {
		return m.renderWithFullscreen(fs[0])
	}

	header := m.renderHeader()

	// Top panels (soul, prompt-preview) slot between header and transcript.
	topRegion := m.renderStackedPanels(posTop)

	// Transcript + left/right side panels compose horizontally.
	transcript := m.renderTranscriptWithSides()

	// Bottom-position panels + the slash drawer share the "above input"
	// strip. The slash drawer is still bespoke (text-triggered not key-
	// toggled); bottom panels stack below it.
	var bottomSections []string
	if m.slashOpen {
		bottomSections = append(bottomSections, m.renderSlashDrawer())
	}
	if bp := m.renderStackedPanels(posBottom); bp != "" {
		bottomSections = append(bottomSections, bp)
	}

	thinTop := m.styles.thinRule.Render(strings.Repeat("─", max(0, m.width)))
	inputLine := m.renderInput()
	thinBot := m.styles.thinRule.Render(strings.Repeat("─", max(0, m.width)))
	status := m.renderStatus()

	sections := []string{header}
	if topRegion != "" {
		sections = append(sections, topRegion)
	}
	sections = append(sections, transcript)
	sections = append(sections, bottomSections...)
	sections = append(sections, thinTop, inputLine, thinBot, status)

	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

// renderWithFullscreen replaces the transcript region with a fullscreen
// panel's view. Header and status-bar stay visible.
func (m model) renderWithFullscreen(spec *panelSpec) string {
	header := m.renderHeader()
	status := m.renderStatus()
	// Reserve: header(2: title + heavy rule) + input-frame(5: thin-top + 3 input rows
	// + thin-bot) + status(1) = 8 total.
	reserved := 2 + 5 + 1
	contentHeight := max(1, m.height-reserved)
	content := spec.View(m.width, contentHeight, &m)
	thinTop := m.styles.thinRule.Render(strings.Repeat("─", max(0, m.width)))
	inputLine := m.renderInput()
	thinBot := m.styles.thinRule.Render(strings.Repeat("─", max(0, m.width)))
	return lipgloss.JoinVertical(lipgloss.Left,
		header, content, thinTop, inputLine, thinBot, status)
}

// renderStackedPanels returns the rendered panels at a given position
// stacked vertically, or "" if none. Used for top and bottom regions.
func (m model) renderStackedPanels(pos panelPosition) string {
	specs := m.panelsAt(pos)
	if len(specs) == 0 {
		return ""
	}
	parts := make([]string, 0, len(specs))
	for _, s := range specs {
		h := s.PreferredHeight
		if h == 0 {
			h = 8
		}
		parts = append(parts, s.View(m.width, h, &m))
	}
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

// renderTranscriptWithSides composes the transcript with any left/right
// panels. If no side panels are open, it's just the transcript viewport.
func (m model) renderTranscriptWithSides() string {
	left := m.panelsAt(posLeft)
	right := m.panelsAt(posRight)

	if len(left) == 0 && len(right) == 0 {
		return m.viewport.View()
	}

	// Allocate widths. Side panels take their PreferredWidth (default 32);
	// transcript takes the remainder.
	leftW := 0
	if len(left) > 0 {
		leftW = left[0].PreferredWidth
		if leftW == 0 {
			leftW = 32
		}
	}
	rightW := 0
	if len(right) > 0 {
		rightW = right[0].PreferredWidth
		if rightW == 0 {
			rightW = 32
		}
	}
	transcriptW := max(10, m.width-leftW-rightW)

	// Re-set viewport width so content reflows correctly while panels are
	// open. We restore when they close via relayout().
	m.viewport.Width = transcriptW
	m.viewport.SetContent(m.transcript.String())
	h := m.viewport.Height

	parts := []string{}
	if len(left) > 0 {
		leftContent := left[0].View(leftW, h, &m)
		// Vertical divider between panel and transcript.
		divider := strings.Repeat(m.styles.thinRule.Render("│")+"\n", max(1, h))
		parts = append(parts, leftContent, divider)
	}
	parts = append(parts, m.viewport.View())
	if len(right) > 0 {
		rightContent := right[0].View(rightW, h, &m)
		divider := strings.Repeat(m.styles.thinRule.Render("│")+"\n", max(1, h))
		parts = append(parts, divider, rightContent)
	}

	return lipgloss.JoinHorizontal(lipgloss.Top, parts...)
}

// renderSlashDrawer draws the filtered command list above the input. Each
// row shows the slash-prefixed name, any hotkey, and the description.
// Selected row is highlighted.
func (m model) renderSlashDrawer() string {
	var b strings.Builder
	// Light rule above the drawer so it feels attached to the input frame
	// rather than hanging off the transcript.
	b.WriteString(m.styles.thinRule.Render(strings.Repeat("─", max(0, m.width))))
	b.WriteByte('\n')

	if len(m.slashMatches) == 0 {
		b.WriteString(m.styles.statusHint.Render("  no matching commands"))
		b.WriteByte('\n')
		b.WriteString(m.styles.statusHint.Render("  esc cancel"))
		return b.String()
	}

	maxRows := drawerHeight(&m) - 2 // minus rule + footer
	if maxRows < 1 {
		maxRows = 1
	}
	if maxRows > len(m.slashMatches) {
		maxRows = len(m.slashMatches)
	}

	// Window around the selected index so selection is always visible.
	start := 0
	if m.slashIndex >= maxRows {
		start = m.slashIndex - maxRows + 1
	}
	end := start + maxRows
	if end > len(m.slashMatches) {
		end = len(m.slashMatches)
	}

	for i := start; i < end; i++ {
		c := m.slashMatches[i]
		row := fmt.Sprintf("  /%-10s  %s", c.Name, c.Description)
		if c.Hotkey != "" {
			row = fmt.Sprintf("  /%-10s  [%s]  %s", c.Name, c.Hotkey, c.Description)
		}
		if i == m.slashIndex {
			// Selected row — invert with surface-hi background.
			sel := lipgloss.NewStyle().
				Foreground(colFocus).
				Background(colSurfaceHi).
				Bold(true)
			b.WriteString(sel.Render(padRight(row, m.width)))
		} else {
			b.WriteString(m.styles.body.Render(row))
		}
		b.WriteByte('\n')
	}
	b.WriteString(m.styles.statusHint.Render("  ↑↓ navigate · enter run · esc cancel"))
	return b.String()
}

// renderHelp moved to panel_help.go as a fullscreen panel. Kept as a
// reference point so the help overlay still exists even if Update's
// routing changes — but this function is no longer called.

func prependSlash(ss []string) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = "/" + s
	}
	return out
}

// padRight space-pads s to width characters for full-row highlighting.
func padRight(s string, width int) string {
	w := lipgloss.Width(s)
	if w >= width {
		return s
	}
	return s + strings.Repeat(" ", width-w)
}

func (m model) renderHeader() string {
	sessionLabel := m.session.Name
	if sessionLabel == "" {
		sessionLabel = fmt.Sprintf("session %d", m.session.ID)
	}
	// Name in soft-white bold (label, not voice), session meta in the dim
	// moonlight echo. Role is NOT drawn here when we're in the baseline
	// thinking_partner state — she's just Selene then. Specialized roles
	// appear as "mode: X" in the accent color.
	parts := []string{m.styles.headerName.Render(m.aiName)}
	parts = append(parts, m.styles.headerMeta.Render("  ·  "+sessionLabel))
	if !isBaselineRole(m.session.Role) {
		parts = append(parts,
			m.styles.headerMeta.Render("  ·  mode: "),
			m.styles.statusMode.Render(m.session.Role))
	}
	title := strings.Join(parts, "")

	// Rule on its own line, heavy horizontal (━), faintly moonlight-tinted.
	// This separates the header from the conversation like a panel divider
	// instead of feeling like it's glued to the last char of the title.
	rule := m.styles.headerRule.Render(strings.Repeat("━", max(0, m.width)))
	return title + "\n" + rule
}

func (m model) renderInput() string {
	glyph := m.styles.inputGlyph.Render("▸ ")
	return glyph + m.input.View()
}

func (m model) renderStatus() string {
	// Stats get the color of their concept: memory is amber, threads are
	// lavender, mode is role-accent. Label and number share the same color
	// so they read as one linked unit; the number is bold to punch, the
	// label stays non-bold as a quieter suffix.
	var b strings.Builder

	// Streaming indicator — when Selene is mid-turn, render a breathing
	// "• thinking" so the user sees presence even during silences (thinking
	// phase, tool execution). Low-amplitude pulse: presence should feel calm.
	if m.streaming {
		b.WriteString(renderStreamingPulse(m.tickCounter))
		b.WriteString(m.styles.statusHint.Render("  ·  "))
	}

	if !isBaselineRole(m.session.Role) {
		b.WriteString(m.styles.statusMode.Render("mode: " + m.session.Role))
		b.WriteString(m.styles.statusHint.Render("  ·  "))
	}
	b.WriteString(m.styles.statusMemNum.Render(fmt.Sprintf("%d", m.memoryCount)))
	b.WriteString(m.styles.statusMemLbl.Render(" mem"))
	if m.threadCount > 0 {
		b.WriteString(m.styles.statusHint.Render("  ·  "))
		// Animated spinner for running threads — four-frame rotation synced
		// to the tick counter. Aligned with the static ◇ idle form.
		b.WriteString(m.styles.statusThrLbl.Render(threadSpinnerFrame(m.tickCounter) + " "))
		b.WriteString(m.styles.statusThrNum.Render(fmt.Sprintf("%d thread", m.threadCount)))
	}
	// Status hint invites discovery: ? for help, ^q to quit, / to browse
	// commands. Kept terse so it doesn't compete with the stats.
	hint := m.styles.statusHint.Render("    ?  help   ·   /  commands   ·   ^q  quit")

	// No rule here — the thin rule above the input (rendered in View())
	// already separates status from input. Status is just the stats line.
	return b.String() + hint
}

// threadSpinnerFrame returns one frame of a four-step rotation synced to
// the tick counter. The forms are all diamond-family unicode so the
// spinner aligns visually with the idle ◇ form.
func threadSpinnerFrame(tick int) string {
	frames := []string{"◇", "◈", "◆", "◈"}
	return frames[tick%len(frames)]
}

// renderStreamingPulse returns "• thinking" with the bullet alternating
// bright/dim on each tick. Low-amplitude on purpose — presence should feel
// calm, not frantic. The word stays dim throughout so it reads as metadata.
func renderStreamingPulse(tick int) string {
	bright := lipgloss.NewStyle().Foreground(colVoiceSelene).Bold(true)
	dim := lipgloss.NewStyle().Foreground(colVoiceSelene2)
	bullet := "•"
	if tick%2 == 0 {
		return bright.Render(bullet) + " " + dim.Render("thinking")
	}
	return dim.Render(bullet) + " " + dim.Render("thinking")
}

// --- transcript helpers ---

func (m *model) appendUser(text string) {
	fmt.Fprintf(m.transcript, "%s%s\n\n",
		m.styles.voiceUser.Render("You: "),
		m.styles.body.Render(text))
	m.pushViewport()
}

func (m *model) appendSystem(text string) {
	fmt.Fprintf(m.transcript, "%s\n\n",
		m.styles.voiceSystem.Render(text))
	m.pushViewport()
}

func (m *model) startAssistant() {
	fmt.Fprintf(m.transcript, "%s",
		m.styles.voiceSelene.Render(m.aiName+": "))
	m.streaming = true
	m.pushViewport()
}

func (m *model) appendAssistantToken(tok string) {
	m.transcript.WriteString(m.styles.body.Render(tok))
	m.pushViewport()
}

func (m *model) finishAssistant() {
	m.transcript.WriteString("\n\n")
	m.pushViewport()
}

func (m *model) appendToolStart(name, argsPreview string) {
	if argsPreview != "" {
		argsPreview = " " + argsPreview
	}
	line := fmt.Sprintf("  ▸ %s%s", name, argsPreview)
	fmt.Fprintf(m.transcript, "%s", m.styles.toolLine.Render(line))
	m.pushViewport()
}

func (m *model) appendToolEnd(ok bool) {
	marker := m.styles.toolOK.Render(" ✓")
	if !ok {
		marker = m.styles.toolErr.Render(" ✗")
	}
	fmt.Fprintf(m.transcript, "%s\n", marker)
	m.pushViewport()
}

func (m *model) pushViewport() {
	// Only auto-scroll if the user hasn't scrolled up to read earlier turns.
	// AtBottom() reports whether we were pinned to the bottom before the
	// content grew; if so, follow the stream. Otherwise preserve their scroll
	// position — they're reading something and don't want to get yanked.
	wasAtBottom := m.viewport.AtBottom()
	m.viewport.SetContent(m.transcript.String())
	if wasAtBottom {
		m.viewport.GotoBottom()
	}
}

// --- event handling ---

func (m *model) handleEvent(ev agent.Event) {
	switch ev.Type {
	case agent.EventTokens:
		p := ev.Payload.(agent.PayloadTokens)
		m.appendAssistantToken(p.Token)
	case agent.EventToolStart:
		p := ev.Payload.(agent.PayloadToolStart)
		m.appendToolStart(p.Name, summarizeArgs(p.Args))
	case agent.EventToolEnd:
		p := ev.Payload.(agent.PayloadToolEnd)
		m.appendToolEnd(!p.IsError)
	case agent.EventAgentEnd:
		m.finishAssistant()
	}
}

// summarizeArgs renders a compact one-line preview of tool args. For v1 we
// just show a single representative field if present (path, query, content
// truncated) — otherwise omit. Keeps the tool line short.
func summarizeArgs(args map[string]any) string {
	for _, key := range []string{"path", "query", "action", "name", "id"} {
		if v, ok := args[key]; ok && v != nil {
			return fmt.Sprintf("%v", v)
		}
	}
	return ""
}

// --- msg types ---

type eventMsg struct {
	event agent.Event
}

type promptErrMsg struct {
	err error
}

type turnCompleteMsg struct{}

// listenEvents returns a tea.Cmd that blocks on the event channel and returns
// a tea.Msg wrapping the event. The Update loop re-issues it after each
// event to keep the pump going. If the channel closes, the cmd returns nil
// (which terminates the pump — intentional on shutdown).
func listenEvents(ch <-chan agent.Event) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return nil
		}
		return eventMsg{event: ev}
	}
}

