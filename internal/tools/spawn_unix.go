//go:build !windows

package tools

import "syscall"

// detached returns SysProcAttr that creates a new process group,
// so the subprocess survives its parent's terminal closing.
func detached() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}
