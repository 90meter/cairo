package providers

import "os/exec"

// WshProvider detects Wave Terminal's wsh command at construction time.
// If wsh is found, it contributes a hint about available wsh commands.
type WshProvider struct {
	hint string // cached at construction, "" if wsh not found
}

// NewWshProvider constructs a WshProvider. Detection runs once here.
func NewWshProvider() *WshProvider {
	p := &WshProvider{}
	if _, err := exec.LookPath("wsh"); err == nil {
		p.hint = "You are running within Wave Terminal (wsh). Use `wsh view <file>`, `wsh edit <file>`, or `wsh browser <url>` to collaborate. Run `wsh -h` to learn more about available commands.\n"
	}
	return p
}

func (p *WshProvider) Name() string { return "wsh" }

// Context returns the cached wsh hint, or "" if wsh is not available.
func (p *WshProvider) Context(_ string) string { return p.hint }
