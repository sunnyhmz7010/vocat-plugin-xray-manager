//go:build linux

package engine

import (
	"os/exec"
	"syscall"
)

// 宿主强制终止插件时，内核也随父进程退出，避免孤儿监听端口。
func configureProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
}
