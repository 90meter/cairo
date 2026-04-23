package tui

// panel_files.go — bottom drawer that wraps bubbles/filepicker. Lets the
// user browse the session CWD and insert an @<path> reference into the
// input without having to type the path. Triggers two ways:
//
//   Ctrl-O                  always
//   @ (typed as first char  convenience — picks up from the @ they
//      or after whitespace)  already typed, so the common flow "look at
//                            @<autocomplete>" feels seamless.
//
// On selection, the panel turns the absolute path bubbles returns into a
// session-relative path, finds the triggering @ in the input (if any), and
// inserts the ref right after it. If there's no @ in the input yet, it
// prepends one. Either way the user ends up with @path/to/file in context.

import (
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/filepicker"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const panelFilesID panelID = "files"

type filesState struct {
	picker filepicker.Model
	height int // used by View to set picker's visible rows
}

func init() {
	registerPanel(&panelSpec{
		ID:              panelFilesID,
		Position:        posBottom,
		Accent:          colVoiceSystem, // soft lavender — a browsing/navigation tone
		Title:           "files",
		Description:     "Browse local files. Enter inserts an @reference into the message; Esc closes without inserting.",
		ToggleKey:       "ctrl+o",
		ShowInHelp:      true,
		PreferredHeight: 12,
		OnOpen:          filesOpen,
		Update:          filesUpdate,
		View:            filesView,
	})
}

// filesOpen initializes a fresh filepicker rooted at the session CWD and
// returns its Init cmd so the directory read actually fires.
func filesOpen(m *model) tea.Cmd {
	p := filepicker.New()
	p.CurrentDirectory = m.session.CWD
	p.ShowHidden = false
	p.DirAllowed = false
	p.FileAllowed = true
	// Leave AllowedTypes empty — any file is selectable. Users can browse
	// binaries if they want; the @file expander will skip them cleanly
	// with a "binary file — skipped" note in the appendix.
	p.Height = 10
	p.Styles = filepicker.DefaultStyles()
	m.files.picker = p
	m.files.height = 10
	return p.Init()
}

// filesUpdate forwards key and internal messages to the filepicker, watches
// for selection, and closes on Esc. Claims all key messages while the panel
// is focused so arrow keys navigate the picker rather than the input.
func filesUpdate(msg tea.Msg, m *model) (bool, tea.Cmd) {
	// Always forward to the picker — internal messages like readDirMsg
	// need to reach it to complete async directory loads.
	newPicker, cmd := m.files.picker.Update(msg)
	m.files.picker = newPicker

	// Selection check. The picker owns the state; DidSelectFile inspects
	// the just-processed message.
	if didSelect, path := m.files.picker.DidSelectFile(msg); didSelect {
		insertFileRef(m, path)
		m.closePanel(panelFilesID)
		return true, nil
	}

	// Esc closes. Also handle escape-like ctrl+c — slash-drawer convention.
	if key, ok := msg.(tea.KeyMsg); ok {
		if s := key.String(); s == "esc" {
			m.closePanel(panelFilesID)
			return true, nil
		}
		// Claim all other keys so arrows don't leak to the input.
		return true, cmd
	}
	// Non-key messages (WindowSize, timers, readDir): don't claim, let
	// the main Update continue its work too. Return the picker's cmd so
	// its internal state advances.
	return false, cmd
}

func filesView(width, height int, m *model) string {
	// Fit the picker to the panel box. Reserve 2 rows for header + footer.
	pickerHeight := max(4, height-2)
	m.files.picker.Height = pickerHeight

	accent := lipgloss.NewStyle().Foreground(colVoiceSystem).Bold(true)
	title := accent.Render("files  ") +
		m.styles.statusHint.Render(shortenDir(m.files.picker.CurrentDirectory, width-10))

	body := m.files.picker.View()

	hint := m.styles.statusHint.Render("  ↑↓ navigate · enter select · esc close")

	top := m.styles.thinRule.Render(strings.Repeat("─", max(0, width)))
	return top + "\n" + title + "\n" + body + "\n" + hint
}

// shortenDir abbreviates deep paths with "…/" prefix to fit in w columns.
func shortenDir(dir string, w int) string {
	if w < 10 {
		return ""
	}
	if len(dir) <= w {
		return dir
	}
	// Drop leading segments, prefix with "…/".
	parts := strings.Split(dir, string(filepath.Separator))
	for i := range parts {
		candidate := "…/" + strings.Join(parts[i+1:], string(filepath.Separator))
		if len(candidate) <= w {
			return candidate
		}
	}
	return "…" + dir[len(dir)-w+1:]
}

// insertFileRef rewrites the input field to include an @<relative-path>
// reference to the selected absolute file path. Strategy:
//   - Resolve relative to session CWD (or keep absolute if it escapes).
//   - If the input already ends with "@", insert the path right after it.
//   - Otherwise append " @<path> " (with leading space as needed).
//   - Cursor moves to the end for the natural "keep typing" flow.
func insertFileRef(m *model, absPath string) {
	rel, err := filepath.Rel(m.session.CWD, absPath)
	if err != nil || strings.HasPrefix(rel, "..") {
		// Escape from CWD — use absolute. Expand-time safety still applies:
		// expandFileRefs enforces under-CWD and will drop the @ref cleanly
		// if the path sneaks out.
		rel = absPath
	}

	v := m.input.Value()
	switch {
	case strings.HasSuffix(v, "@"):
		// Picker was opened from a just-typed @; insert the path right after.
		m.input.SetValue(v + rel + " ")
	case v == "":
		m.input.SetValue("@" + rel + " ")
	case strings.HasSuffix(v, " "):
		m.input.SetValue(v + "@" + rel + " ")
	default:
		m.input.SetValue(v + " @" + rel + " ")
	}
	// textarea.SetValue resets then re-inserts, which leaves the cursor at
	// the end of the inserted content — exactly where we want it.
}
