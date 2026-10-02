//go:build !linux

package engine

import "os/exec"

func configureProcess(cmd *exec.Cmd) {}
