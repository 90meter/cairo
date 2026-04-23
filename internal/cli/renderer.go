package cli

import (
	"fmt"
	"os"

	"github.com/scotmcc/cairo/internal/agent"
)

// Renderer subscribes to the agent bus and prints events to stdout.
// This is the CLI's "TUI" — replace with a real BubbleTea subscriber later.
func Renderer(bus *agent.Bus) (stop func()) {
	ch, unsub := bus.Subscribe()

	go func() {
		for event := range ch {
			switch event.Type {

			case agent.EventTokens:
				p := event.Payload.(agent.PayloadTokens)
				fmt.Print(p.Token)

			case agent.EventThinking:
				// dim thinking output
				p := event.Payload.(agent.PayloadThinking)
				fmt.Fprintf(os.Stderr, "\033[2m%s\033[0m", p.Token)

			case agent.EventToolStart:
				p := event.Payload.(agent.PayloadToolStart)
				fmt.Printf("\n\033[33m⚙ %s\033[0m ", p.Name)

			case agent.EventToolEnd:
				p := event.Payload.(agent.PayloadToolEnd)
				if p.IsError {
					fmt.Printf("\033[31m[error]\033[0m\n")
				} else {
					fmt.Printf("\033[32m[done]\033[0m\n")
				}

			case agent.EventAgentStart:
				// nothing — prompt already printed by CLI

			case agent.EventTurnEnd:
				fmt.Println() // newline after streamed response

			case agent.EventError:
				p := event.Payload.(agent.PayloadError)
				fmt.Fprintf(os.Stderr, "\n\033[31merror: %v\033[0m\n", p.Err)
			}
		}
	}()

	return unsub
}
