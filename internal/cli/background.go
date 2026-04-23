package cli

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/scotmcc/cairo/internal/agent"
)

// BackgroundRenderer subscribes to the agent bus and writes plain text to w.
// Used by background task workers — no ANSI, no interactive UI.
func BackgroundRenderer(bus *agent.Bus, w io.Writer) (stop func()) {
	ch, unsub := bus.Subscribe()

	go func() {
		for event := range ch {
			switch event.Type {

			case agent.EventAgentStart:
				fmt.Fprintf(w, "[%s] agent started\n", timestamp())

			case agent.EventTurnStart:
				fmt.Fprintf(w, "[%s] turn start\n", timestamp())

			case agent.EventTokens:
				p := event.Payload.(agent.PayloadTokens)
				fmt.Fprint(w, p.Token)

			case agent.EventToolStart:
				p := event.Payload.(agent.PayloadToolStart)
				fmt.Fprintf(w, "\n[%s] tool: %s\n", timestamp(), p.Name)

			case agent.EventToolEnd:
				p := event.Payload.(agent.PayloadToolEnd)
				status := "done"
				if p.IsError {
					status = "error"
				}
				fmt.Fprintf(w, "[%s] tool: %s [%s]\n", timestamp(), p.Name, status)

			case agent.EventTurnEnd:
				fmt.Fprintf(w, "\n[%s] turn end\n", timestamp())

			case agent.EventAgentEnd:
				fmt.Fprintf(w, "[%s] agent finished\n", timestamp())

			case agent.EventError:
				p := event.Payload.(agent.PayloadError)
				fmt.Fprintf(w, "[%s] ERROR: %v\n", timestamp(), p.Err)
			}
		}
	}()

	return unsub
}

// OpenTaskLog opens (or creates) the log file for a background task.
// Falls back to os.Stderr if the path can't be opened.
func OpenTaskLog(logPath string) (*os.File, error) {
	return os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
}

func timestamp() string {
	return time.Now().Format("15:04:05")
}
