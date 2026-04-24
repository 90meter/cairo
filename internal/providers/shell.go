package providers

import "os"

// ShellProvider reads the user's $SHELL environment variable at construction
// time and injects a one-line note about the active shell. Returns "" if
// SHELL is unset.
type ShellProvider struct {
	hint string // cached at construction, "" if SHELL unset
}

// NewShellProvider constructs a ShellProvider. Detection runs once here.
func NewShellProvider() *ShellProvider {
	p := &ShellProvider{}
	if shell := os.Getenv("SHELL"); shell != "" {
		p.hint = "You are running in " + shell + ".\n"
	}
	return p
}

func (p *ShellProvider) Name() string { return "shell" }

// Context returns the cached shell hint, or "" if SHELL was not set.
func (p *ShellProvider) Context(_ string) string { return p.hint }
