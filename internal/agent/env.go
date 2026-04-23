package agent

import (
	"os"
	"os/exec"
)

// Environment captures the detected runtime environment.
type Environment struct {
	IsWsh    bool
	IsVsCode bool
	Shell    string
}

// detectEnvironment inspects the host for common development indicators.
func detectEnvironment() Environment {
	wshFound := false
	if _, err := exec.LookPath("wsh"); err == nil {
		wshFound = true
	}

	vsCodeFound := false
	if _, err := exec.LookPath("code"); err == nil {
		vsCodeFound = true
	}

	return Environment{
		IsWsh:    wshFound,
		IsVsCode: vsCodeFound,
		Shell:    os.Getenv("SHELL"),
	}
}
