package tui

// panel_help.go — fullscreen help overlay. Shows special keys, slash
// commands, and registered panels with their toggle bindings. Any key
// dismisses it.

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const panelHelpID panelID = "help"

func init() {
	registerPanel(&panelSpec{
		ID:          panelHelpID,
		Position:    posFullscreen,
		Accent:      colVoiceSelene,
		Title:       "help",
		Description: "Show keyboard shortcuts, slash commands, and panels.",
		ToggleKey:   "?", // opened via ? on empty input (handled in tui.go),
		// or explicitly via /help.
		ShowInHelp: false, // the help panel itself doesn't need to list itself
		Update:     helpUpdate,
		View:       helpView,
	})
}

// helpUpdate dismisses the overlay on any key. Update returns handled=true
// for every key so help truly acts as a modal — nothing falls through.
func helpUpdate(msg tea.Msg, m *model) (bool, tea.Cmd) {
	if _, ok := msg.(tea.KeyMsg); ok {
		m.closePanel(panelHelpID)
		return true, nil
	}
	return false, nil
}

func helpView(width, _ int, m *model) string {
	var b strings.Builder

	title := m.styles.headerName.Render(m.aiName + " — help")
	b.WriteString(title)
	b.WriteByte('\n')
	b.WriteString(m.styles.headerRule.Render(strings.Repeat("━", max(0, width))))
	b.WriteString("\n\n")

	// Special keys — context-sensitive behavior documented up front.
	b.WriteString(m.styles.statusMode.Render("  Keys"))
	b.WriteString("\n\n")
	specialKeys := []struct{ key, desc string }{
		{"Ctrl-C", "context-aware: cancel turn if Selene is replying · clear input if typed · otherwise clear transcript view"},
		{"Ctrl-D", "EOF — quit if input is empty"},
		{"Ctrl-Q", "quit explicitly"},
		{"?", "this help (when input is empty)"},
		{"/", "open the command drawer (when input is empty)"},
		{"Enter", "send message · execute selected command · dismiss help"},
		{"Esc", "close focused panel or overlay"},
		{"↑ ↓ PgUp PgDn", "scroll the transcript (when input is empty and no panel focused)"},
	}
	for _, k := range specialKeys {
		fmt.Fprintf(&b, "    %s   %s\n",
			m.styles.statusMemLbl.Render(padRight(k.key, 16)),
			m.styles.body.Render(k.desc))
	}
	b.WriteString("\n")

	// Panels — everything registered that opts into help.
	if panels := helpablePanels(); len(panels) > 0 {
		b.WriteString(m.styles.statusMode.Render("  Panels"))
		b.WriteString("\n\n")
		for _, p := range panels {
			name := titleCase(p.Title)
			accent := p.Accent
			if accent == "" {
				accent = colVoiceSelene
			}
			title := lipgloss.NewStyle().Foreground(accent).Bold(true).Render(name)
			hotkey := ""
			if p.ToggleKey != "" {
				hotkey = m.styles.statusMemLbl.Render("   " + p.ToggleKey)
			}
			fmt.Fprintf(&b, "    %s%s\n", title, hotkey)
			fmt.Fprintf(&b, "      %s\n\n", m.styles.body.Render(p.Description))
		}
	}

	// Input-prefixes, since they're conceptually commands without being
	// slash-based.
	b.WriteString(m.styles.statusMode.Render("  Prefixes"))
	b.WriteString("\n\n")
	prefixes := []struct{ key, desc string }{
		{"!<cmd>", "run a shell command in the session CWD; output becomes the user turn"},
		{"@<path>", "reference a file — Selene sees its contents appended, your transcript stays clean"},
	}
	for _, p := range prefixes {
		fmt.Fprintf(&b, "    %s   %s\n",
			m.styles.statusMemLbl.Render(padRight(p.key, 16)),
			m.styles.body.Render(p.desc))
	}
	b.WriteString("\n")

	// Slash commands.
	b.WriteString(m.styles.statusMode.Render("  Commands"))
	b.WriteString("\n\n")
	cmds := filterCommands(m.commands, "")
	for _, c := range cmds {
		name := m.styles.statusMode.Render(fmt.Sprintf("/%s", c.Name))
		aliases := ""
		if len(c.Aliases) > 0 {
			aliases = m.styles.statusHint.Render(
				" (" + strings.Join(prependSlash(c.Aliases), ", ") + ")")
		}
		hotkey := ""
		if c.Hotkey != "" {
			hotkey = m.styles.statusMemLbl.Render("   " + c.Hotkey)
		}
		fmt.Fprintf(&b, "    %s%s%s\n", name, aliases, hotkey)
		fmt.Fprintf(&b, "      %s\n\n", m.styles.body.Render(c.Description))
	}

	b.WriteString(m.styles.statusHint.Render("  any key to dismiss"))
	return b.String()
}

func titleCase(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
