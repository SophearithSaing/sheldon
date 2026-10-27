//go:build linux || darwin

package process

import (
	"errors"
	"os/exec"
	"syscall"
)

type processTree struct{ cmd *exec.Cmd }

// prepareTree configures cmd to start in its own process group.
func prepareTree(cmd *exec.Cmd) (*processTree, error) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return &processTree{cmd: cmd}, nil
}

// started requires no additional setup after a Unix child starts.
func (t *processTree) started() error { return nil }

// terminate requests shutdown of the owned process group with SIGTERM.
func (t *processTree) terminate() error { return t.signal(syscall.SIGTERM) }

// kill forcibly stops the owned process group with SIGKILL.
func (t *processTree) kill() error { return t.signal(syscall.SIGKILL) }

// close requires no cleanup on Unix because process groups have no owned handle.
func (t *processTree) close() error { return nil }

// signal sends a signal to the owned process group, ignoring an exited group.
func (t *processTree) signal(signal syscall.Signal) error {
	err := syscall.Kill(-t.cmd.Process.Pid, signal)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}
