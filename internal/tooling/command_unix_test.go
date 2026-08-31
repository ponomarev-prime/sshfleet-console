//go:build !windows

package tooling

import (
	"os/exec"
	"syscall"
)

func isolateTestCommand(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
