package tui

// commands.go — one registry for every command-like action in the TUI.
// Each entry carries its slash name, any aliases, an optional hotkey, a
// short description, and the handler. The slash drawer, the hotkey
// dispatcher, and the help overlay all read from this same table — adding a
// new command is one struct literal that surfaces everywhere.
//
// Design rule held as law elsewhere in cairo: the user's TUI actions do not
// mutate the DB. Commands that affect Selene's mind (memories, soul,
// prompts, session history) must go through conversation. Commands here
// only touch UI state (view, layout, drawers) or initiate conversation.

import (
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// Command is a single invokable action. Handler returns a tea.Cmd (which
// may be nil) and may set any model state it needs through the pointer.
type Command struct {
	Name        string                     // e.g. "quit"
	Aliases     []string                   // e.g. {"q", "exit"}
	Hotkey      string                     // e.g. "ctrl+q"; empty means no hotkey
	Description string                     // one-line for drawer + help
	Handler     func(*model) tea.Cmd
}

// defaultCommands returns the built-in command registry.
func defaultCommands() []Command {
	return []Command{
		{
			Name:        "quit",
			Aliases:     []string{"q", "exit"},
			Hotkey:      "ctrl+q",
			Description: "Close cairo. Drains the background summarizer before exit.",
			Handler: func(_ *model) tea.Cmd {
				return tea.Quit
			},
		},
		{
			Name:        "clear",
			// Ctrl-C is handled with state-dependent semantics directly in
			// Update (cancel → clear input → clear view), not via the
			// registry. /clear here is the unconditional "always clear
			// the view" form, intentionally hotkey-less.
			Description: "Clear the visible transcript. Selene's memory is untouched — this is view-only.",
			Handler: func(m *model) tea.Cmd {
				m.transcript.Reset()
				m.pushViewport()
				return nil
			},
		},
		{
			Name:        "help",
			Aliases:     []string{"?"},
			Description: "Show commands and hotkeys. Esc to dismiss.",
			Handler: func(m *model) tea.Cmd {
				return m.openPanel(panelHelpID)
			},
		},
		{
			Name:        "init",
			Description: "Start the guided setup. Selene introduces herself, captures your name, learns the project.",
			Handler: func(m *model) tea.Cmd {
				// Fire the init flow by submitting the same prompt the CLI does.
				// Future: move the init-prompt builder into a shared package so
				// both CLI and TUI use it.
				text := initPromptFor(m)
				m.appendUser("(starting initialization)")
				m.startAssistant()
				return m.submit(text)
			},
		},
	}
}

// initPromptFor loads the init skill from the DB and returns the text to
// send as the user's opening message. Mirrors cli.buildInitPrompt but scoped
// to the TUI's assumptions.
func initPromptFor(m *model) string {
	skill, err := m.db.Skills.Get("init")
	if err != nil || skill == nil {
		return "Please run an initialization process: introduce yourself, ask what I should be called, then capture project context using memory, prompt_part, and config tools."
	}
	// Also queue a follow-up to guarantee the storage step even if the
	// model skips it inline during the main turn.
	m.agent.FollowUp("Storage step: call config(action=\"set\", key=\"user_name\", value=<their name>) if not already done, then memory(action=\"add\") for each distinct fact learned, then config(action=\"set\", key=\"init_complete\", value=\"true\").")
	return "Please run the initialization process now. Follow the guidance carefully.\n\n" + skill.Content
}

// lookupByName returns a pointer to the command whose Name or Aliases matches
// the given bare token (no leading slash). Returns nil if no match.
func lookupByName(cmds []Command, token string) *Command {
	token = strings.ToLower(strings.TrimSpace(token))
	if token == "" {
		return nil
	}
	for i := range cmds {
		if strings.EqualFold(cmds[i].Name, token) {
			return &cmds[i]
		}
		for _, a := range cmds[i].Aliases {
			if strings.EqualFold(a, token) {
				return &cmds[i]
			}
		}
	}
	return nil
}

// lookupByHotkey returns the command bound to the given tea key string
// (e.g. "ctrl+q"). Returns nil if no command has that binding.
func lookupByHotkey(cmds []Command, hotkey string) *Command {
	if hotkey == "" {
		return nil
	}
	for i := range cmds {
		if cmds[i].Hotkey == hotkey {
			return &cmds[i]
		}
	}
	return nil
}

// filterCommands returns commands matching query. Substring-match over Name
// and Aliases (case-insensitive); prefix matches are ranked before
// mid-string matches; within rank, alphabetical. Empty query returns all.
func filterCommands(cmds []Command, query string) []Command {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		out := make([]Command, len(cmds))
		copy(out, cmds)
		sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
		return out
	}

	type scored struct {
		cmd  Command
		rank int // 0 = prefix, 1 = substring; lower is better
	}
	var matches []scored
	for _, c := range cmds {
		rank := -1
		// Check name first
		n := strings.ToLower(c.Name)
		if strings.HasPrefix(n, query) {
			rank = 0
		} else if strings.Contains(n, query) {
			rank = 1
		}
		// Check aliases — a prefix match on an alias still counts.
		for _, a := range c.Aliases {
			al := strings.ToLower(a)
			if strings.HasPrefix(al, query) && (rank == -1 || rank > 0) {
				rank = 0
			} else if strings.Contains(al, query) && rank == -1 {
				rank = 1
			}
		}
		if rank >= 0 {
			matches = append(matches, scored{c, rank})
		}
	}
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].rank != matches[j].rank {
			return matches[i].rank < matches[j].rank
		}
		return matches[i].cmd.Name < matches[j].cmd.Name
	})
	out := make([]Command, len(matches))
	for i, m := range matches {
		out[i] = m.cmd
	}
	return out
}
