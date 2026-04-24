package providers

// Provider is a small, focused component that contributes context to the
// system prompt. Each provider knows how to detect its own relevance and
// returns a snippet to inject, or an empty string if it has nothing to add.
//
// Providers that detect static state (e.g. is wsh installed?) do so once at
// construction and cache the result. Providers that need fresh data (e.g. git
// branch) compute on each call to Context.
type Provider interface {
	// Name returns a stable identifier for the provider, used for ordering
	// and diagnostics. Convention: lowercase, no spaces.
	Name() string

	// Context returns the text to inject into the system prompt, or "" if
	// this provider has nothing to contribute. cwd is the agent's current
	// working directory, passed so per-directory providers (e.g. git) can
	// operate correctly.
	Context(cwd string) string
}
