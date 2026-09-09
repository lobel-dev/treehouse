//go:build !windows

package gitvcs

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

func configureFetchCancellation(cmd *exec.Cmd) {
	// Git's SSH/remote helpers inherit this group, never the caller's group.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
}
