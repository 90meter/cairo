package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/scotmcc/cairo/internal/agent"
	"github.com/scotmcc/cairo/internal/db"
)

// Run starts the interactive CLI chat loop.
func Run(a *agent.Agent, database *db.DB, session *db.Session) error {
	stop := Renderer(a.Bus())
	defer stop()
	defer a.Close() // drain summarizer before exit

	printBanner(database, session)
	maybeInitHint(database)

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 1<<20), 1<<20)

	for {
		fmt.Print("\033[36m> \033[0m")

		if !scanner.Scan() {
			fmt.Println()
			break
		}

		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "/") {
			exit, prompt := handleCommand(line, a, database, session)
			if exit {
				break
			}
			if prompt != "" {
				if err := a.Prompt(context.Background(), prompt); err != nil {
					fmt.Fprintf(os.Stderr, "error: %v\n", err)
				}
			}
			continue
		}

		if err := a.Prompt(context.Background(), line); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
		}
	}

	return scanner.Err()
}

// RunOnce sends a single message and exits — for scripted use.
// Waits for background work (summarizer) to complete before returning.
func RunOnce(a *agent.Agent, text string) error {
	stop := Renderer(a.Bus())
	defer stop()
	err := a.Prompt(context.Background(), text)
	a.Close() // drain summarizer goroutine before process exits
	return err
}

// handleCommand processes a slash command.
// Returns (exit, agentPrompt) — if agentPrompt is non-empty, it is sent to the agent.
func handleCommand(line string, a *agent.Agent, database *db.DB, session *db.Session) (exit bool, agentPrompt string) {
	parts := strings.Fields(line)
	cmd := parts[0]
	arg := ""
	if len(parts) > 1 {
		arg = strings.Join(parts[1:], " ")
	}

	switch cmd {
	case "/exit", "/quit", "/q":
		fmt.Println("bye.")
		return true, ""

	case "/init":
		return false, buildInitPrompt(arg, a, database, session)

	case "/session":
		printSession(session)

	case "/sessions":
		sessions, err := database.Sessions.List()
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return
		}
		if len(sessions) == 0 {
			fmt.Println("no sessions")
			return
		}
		for _, s := range sessions {
			marker := "  "
			if s.ID == session.ID {
				marker = "* "
			}
			name := s.Name
			if name == "" {
				name = "(unnamed)"
			}
			fmt.Printf("%s[%d] %s — %s — %s\n",
				marker, s.ID, name, s.Role, s.LastActive.Format("2006-01-02 15:04"))
		}

	case "/jobs":
		jobs, err := database.Jobs.List()
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return
		}
		if len(jobs) == 0 {
			fmt.Println("no jobs")
			return
		}
		for _, j := range jobs {
			fmt.Printf("[%d] [%s] %s\n", j.ID, j.Status, j.Title)
		}

	case "/memories":
		memories, err := database.Memories.All()
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return
		}
		if len(memories) == 0 {
			fmt.Println("no memories")
			return
		}
		for _, m := range memories {
			fmt.Printf("[%d] %s\n", m.ID, m.Content)
		}

	case "/tools":
		customTools, err := database.Tools.List()
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return
		}
		if len(customTools) == 0 {
			fmt.Println("no custom tools")
			return
		}
		for _, t := range customTools {
			status := "on"
			if !t.Enabled {
				status = "off"
			}
			fmt.Printf("[%s] %s — %s\n", status, t.Name, t.Description)
		}

	case "/skills":
		skills, err := database.Skills.List()
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return
		}
		if len(skills) == 0 {
			fmt.Println("no skills")
			return
		}
		for _, s := range skills {
			fmt.Printf("%s — %s\n", s.Name, s.Description)
		}

	case "/help":
		fmt.Print(`
slash commands:
  /init              guided setup: learn this project and configure the AI
  /init codebase     explore and learn the current codebase only
  /session           show current session info
  /sessions          list all sessions (restart with -session <id> to switch)
  /jobs              list all jobs
  /memories          list stored memories
  /tools             list custom tools
  /skills            list skills
  /help              show this help
  /exit              exit cairo
`)

	default:
		fmt.Printf("unknown command: %s (try /help)\n", cmd)
	}

	return false, ""
}

// buildInitPrompt loads the appropriate init skill and builds the prompt to send.
// It also injects a follow-up into the agent's queue so that after the exploration
// completes, the agent is forced to store its findings — even if it forgot to do so
// during the main turn.
func buildInitPrompt(arg string, a *agent.Agent, database *db.DB, session *db.Session) string {
	skillName := "init"
	if strings.EqualFold(arg, "codebase") {
		skillName = "init_codebase"
	}

	skill, err := database.Skills.Get(skillName)
	if err != nil || skill == nil {
		return "Please run an initialization process: ask me about this project and my preferences, explore the codebase if there is one, and store what you learn using memory(action=\"add\") and prompt_part(action=\"add\")."
	}

	// Inject a follow-up that fires after the exploration turn completes.
	// This guarantees storage even if the model skipped the memory calls inline.
	// The final step flips the init_complete config flag so the CLI banner stops
	// nudging the user to run init, and later code can distinguish "fresh DB that
	// never ran init" from "DB that ran init and has memories."
	a.FollowUp("Storage step: call memory with action=\"add\" now for every distinct fact you just learned — project purpose, tech stack, architecture, key files, conventions, commands. One call per fact. Then call memory with action=\"list\" to confirm what was stored. Finally, call config with action=\"set\", key=\"init_complete\", value=\"true\" to mark initialization done.")

	var header string
	if skillName == "init_codebase" {
		header = fmt.Sprintf("Working directory: %s\n\nPlease run the codebase exploration process now:\n\n", session.CWD)
	} else {
		header = "Please run the initialization process now. Follow the guidance carefully.\n\n"
	}

	return header + skill.Content
}

func printSession(session *db.Session) {
	name := session.Name
	if name == "" {
		name = "(unnamed)"
	}
	fmt.Printf("session %d: %s\nrole: %s\ncwd:  %s\n", session.ID, name, session.Role, session.CWD)
}

// maybeInitHint prints a one-line nudge after the banner if init_complete is
// still "false" — distinguishing a fresh DB from one that ran init. Stays
// quiet once init_complete=true so it isn't noise on every startup.
func maybeInitHint(database *db.DB) {
	done, _ := database.Config.Get("init_complete")
	if done == "true" {
		return
	}
	aiName, _ := database.Config.Get("ai_name")
	if aiName == "" {
		aiName = "your agent"
	}
	fmt.Printf("\033[2m(%s is here but hasn't met you yet — type /init to introduce yourself, or /config for direct setup)\033[0m\n", aiName)
	fmt.Println()
}

func printBanner(database *db.DB, session *db.Session) {
	name := session.Name
	if name == "" {
		name = fmt.Sprintf("session %d", session.ID)
	}
	aiName, _ := database.Config.Get("ai_name")
	if aiName == "" {
		aiName = "cairo"
	}
	fmt.Printf("\033[1mcairo\033[0m · \033[1m%s\033[0m · %s · role:%s\n", aiName, name, session.Role)
	fmt.Println("type /help for commands, /exit to quit")
	fmt.Println()
}
