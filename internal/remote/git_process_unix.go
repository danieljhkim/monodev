//go:build darwin || linux || freebsd || netbsd || openbsd || dragonfly || solaris

package remote

import (
	"os"
	"os/exec"
	"syscall"
)

func configureGitCommandForContext(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if err == syscall.ESRCH {
			return os.ErrProcessDone
		}
		return err
	}
}
