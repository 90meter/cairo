package providers

import "strings"

// Registry holds an ordered list of Providers and assembles their output into
// a single block for the system prompt. Registration order determines injection
// order — register in the order you want context to appear.
type Registry struct {
	providers []Provider
}

// New returns an empty Registry.
func New() *Registry {
	return &Registry{}
}

// Register appends a provider to the registry. Call in the order providers
// should appear in the system prompt.
func (r *Registry) Register(p Provider) {
	r.providers = append(r.providers, p)
}

// GetContext calls every registered provider with cwd and concatenates all
// non-empty results. Returns "" when no provider has anything to contribute.
func (r *Registry) GetContext(cwd string) string {
	var b strings.Builder
	for _, p := range r.providers {
		if s := p.Context(cwd); s != "" {
			b.WriteString(s)
			if !strings.HasSuffix(s, "\n") {
				b.WriteByte('\n')
			}
		}
	}
	return b.String()
}

// Default returns a Registry pre-loaded with the standard set of environment
// providers: wsh, vscode, shell, git. Detection for wsh/vscode/shell happens
// once here at construction time.
func Default() *Registry {
	r := New()
	r.Register(NewWshProvider())
	r.Register(NewVsCodeProvider())
	r.Register(NewShellProvider())
	r.Register(NewGitProvider())
	return r
}
