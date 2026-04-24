package providers

import "os/exec"

// VsCodeProvider detects Visual Studio Code's `code` CLI at construction time.
// If found, it contributes a hint about using `code` to open files.
type VsCodeProvider struct {
	hint string // cached at construction, "" if code not found
}

// NewVsCodeProvider constructs a VsCodeProvider. Detection runs once here.
func NewVsCodeProvider() *VsCodeProvider {
	p := &VsCodeProvider{}
	if _, err := exec.LookPath("code"); err == nil {
		p.hint = "You are running within VS Code. Use `code -r <file>` to open files, `code <dir>` to open folders. Run `code --help` to learn more.\n"
	}
	return p
}

func (p *VsCodeProvider) Name() string { return "vscode" }

// Context returns the cached VS Code hint, or "" if code is not available.
func (p *VsCodeProvider) Context(_ string) string { return p.hint }
