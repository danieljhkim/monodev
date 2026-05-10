//go:build !(darwin || linux || freebsd || netbsd || openbsd || dragonfly || solaris)

package remote

import "os/exec"

func configureGitCommandForContext(cmd *exec.Cmd) {}
