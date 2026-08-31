//go:build windows

package tooling

import "os/exec"

func isolateTestCommand(_ *exec.Cmd) {}
