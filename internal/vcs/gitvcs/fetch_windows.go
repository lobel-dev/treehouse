//go:build windows

package gitvcs

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"time"
)

func configureFetchCancellation(cmd *exec.Cmd) {
	cmd.Cancel = func() error {
		// Kill the tree before Git exits so taskkill can still discover children.
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		kill := exec.CommandContext(ctx, "taskkill", "/PID", strconv.Itoa(cmd.Process.Pid), "/T", "/F")
		kill.WaitDelay = time.Second
		if out, err := kill.CombinedOutput(); err != nil {
			return errors.Join(fmt.Errorf("kill fetch process tree: %w: %s", err, out), cmd.Process.Kill())
		}
		return nil
	}
}
