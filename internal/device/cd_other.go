//go:build !windows

package device

import "os/exec"

func hideWindow(cmd *exec.Cmd) {}
