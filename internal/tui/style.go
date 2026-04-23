package tui

import "github.com/charmbracelet/lipgloss"

// Moonlight palette — dark background, cool tints, low harshness. Chosen for
// readability at terminal-typical contrast without shouting. Two channels of
// meaning with colors: voice (who's speaking) and accent (what role/mode).

var (
	// Surfaces
	colBg          = lipgloss.Color("#0e1014") // near-black, cool
	colSurface     = lipgloss.Color("#151820") // panels, drawers
	colSurfaceHi   = lipgloss.Color("#1e2230") // overlays, focus
	// Border gets a faint moonlight tint — structural, not a color claim.
	// When drawers land, each drawer's edge will take the color of its
	// concept (amber for memory, lavender for threads, etc.). These bare
	// rules stay subtle-tinted so the reserved vocabulary still belongs to
	// semantic meaning, not frame geometry.
	colBorder      = lipgloss.Color("#3f4a60") // soft moonlight-tinted gray (heavy rules)
	colBorderThin  = lipgloss.Color("#2a3142") // dimmer still — thin rules that offset rather than divide

	// Text
	colText        = lipgloss.Color("#e8e8ef") // soft white (primary)
	colTextMuted   = lipgloss.Color("#a8abbb") // secondary
	colTextDim     = lipgloss.Color("#6a6f7e") // placeholders, metadata

	// Voices — who's speaking
	colVoiceSelene = lipgloss.Color("#a0c8e0") // moonlight-blue
	colVoiceSelene2 = lipgloss.Color("#7090a0") // dim echo of Selene's voice — session labels, her meta
	colVoiceUser   = lipgloss.Color("#d8c890") // warm parchment
	colVoiceSystem = lipgloss.Color("#b098d8") // soft lavender — inbox/bg notes, also threads

	// Semantic colors — the story the UI tells with color. Every time the
	// interface references one of these concepts, the associated color appears.
	// Over time you build peripheral recognition: "amber → memory stuff,"
	// "soft green → tool activity," "lavender → other threads."
	colMemory      = lipgloss.Color("#d4a660") // amber — stored knowledge
	colTool        = lipgloss.Color("#7bc0a0") // soft green — activity, verbs
	colToolBright  = lipgloss.Color("#6fcf97") // brighter green for ✓ markers
	colThread      = lipgloss.Color("#b098d8") // lavender — parallel threads (shares colVoiceSystem)

	// States
	colOK          = lipgloss.Color("#6fcf97")
	colWarn        = lipgloss.Color("#d4a656")
	colErr         = lipgloss.Color("#d77070")
	colFocus       = lipgloss.Color("#ffffff")

	// Role accents — for the prompt glyph and the mode label (when shown).
	// thinking_partner is Selene's baseline state, not a specialized mode,
	// so its color lives on the glyph only — we don't draw a mode label in
	// that state. The others are departures from baseline and get both.
	colAccentSilver  = lipgloss.Color("#c0c0d0") // thinking_partner (baseline glyph)
	colAccentGreen   = lipgloss.Color("#7bc47e") // coder
	colAccentBlue    = lipgloss.Color("#6b9bd2") // planner
	colAccentAmber   = lipgloss.Color("#d4a656") // reviewer
	colAccentMagenta = lipgloss.Color("#c677c7") // orchestrator
)

// isBaselineRole reports whether the given role is Selene's baseline state,
// where no explicit mode label should be drawn — she's just "being Selene."
func isBaselineRole(role string) bool {
	return role == "thinking_partner" || role == ""
}

// roleAccent maps a role name to its accent color. Unknown roles fall back to
// silver (the default thinking-partner tint) so peripheral vision never shows
// an unrecognized color.
func roleAccent(role string) lipgloss.Color {
	switch role {
	case "coder":
		return colAccentGreen
	case "planner":
		return colAccentBlue
	case "reviewer":
		return colAccentAmber
	case "orchestrator":
		return colAccentMagenta
	default:
		return colAccentSilver
	}
}

// styles collects the lipgloss styles used across the TUI. Built once at Run
// time and attached to the model so they can be re-used without re-allocating
// on every View() call.
type styles struct {
	// Top header
	headerName   lipgloss.Style // "Selene" — soft white, bold (label, not voice)
	headerMeta   lipgloss.Style // "session 1" — dim echo of her voice
	headerRule   lipgloss.Style // heavy horizontal — "heading" divider
	thinRule     lipgloss.Style // light horizontal — offset around the input

	// Conversation
	voiceUser    lipgloss.Style
	voiceSelene  lipgloss.Style
	voiceSystem  lipgloss.Style
	body         lipgloss.Style
	toolLine     lipgloss.Style // soft green — tool-call lines
	toolOK       lipgloss.Style // brighter green — ✓ marker
	toolErr      lipgloss.Style // red — ✗ marker

	// Input
	inputGlyph   lipgloss.Style // role-tinted ▸ (silver in baseline)
	input        lipgloss.Style

	// Status bar — semantic colors for the concepts they name
	statusMode   lipgloss.Style // role accent — only drawn when non-baseline
	statusMemNum lipgloss.Style // amber — memory count digits
	statusMemLbl lipgloss.Style // dim — "mem" label
	statusThrNum lipgloss.Style // lavender — thread count digits
	statusThrLbl lipgloss.Style // dim — "◇" marker for threads
	statusHint   lipgloss.Style
	statusRule   lipgloss.Style
}

func newStyles(role string) styles {
	accent := roleAccent(role)
	return styles{
		headerName: lipgloss.NewStyle().
			// Name-as-label is soft-white bold — what felt right the first
			// time. The moonlight-blue voice color lives on "Selene:" when
			// she actually speaks, not on the name tag.
			Foreground(colText).Bold(true),
		headerMeta: lipgloss.NewStyle().
			Foreground(colVoiceSelene2),
		headerRule: lipgloss.NewStyle().
			Foreground(colBorder),
		thinRule: lipgloss.NewStyle().
			// Dimmer than the heavy rules — they're offsets, not section
			// breaks. The hierarchy reads weight→importance twice: thin
			// characters (─) and darker color.
			Foreground(colBorderThin),

		voiceUser: lipgloss.NewStyle().
			Foreground(colVoiceUser).Bold(true),
		voiceSelene: lipgloss.NewStyle().
			Foreground(colVoiceSelene).Bold(true),
		voiceSystem: lipgloss.NewStyle().
			Foreground(colVoiceSystem).Italic(true),
		body: lipgloss.NewStyle().
			Foreground(colText),
		toolLine: lipgloss.NewStyle().
			Foreground(colTool),
		toolOK: lipgloss.NewStyle().
			Foreground(colToolBright).Bold(true),
		toolErr: lipgloss.NewStyle().
			Foreground(colErr).Bold(true),

		inputGlyph: lipgloss.NewStyle().
			Foreground(accent).Bold(true),
		input: lipgloss.NewStyle().
			Foreground(colText),

		statusMode: lipgloss.NewStyle().
			Foreground(accent).Bold(true),
		// Linked colors: number and label share the concept's color so the
		// pair reads as one unit. Number is bold to punch, label is not.
		statusMemNum: lipgloss.NewStyle().
			Foreground(colMemory).Bold(true),
		statusMemLbl: lipgloss.NewStyle().
			Foreground(colMemory),
		statusThrNum: lipgloss.NewStyle().
			Foreground(colThread).Bold(true),
		statusThrLbl: lipgloss.NewStyle().
			Foreground(colThread),
		statusHint: lipgloss.NewStyle().
			Foreground(colTextDim),
		statusRule: lipgloss.NewStyle().
			Foreground(colBorder),
	}
}
