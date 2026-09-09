package gitvcs

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// Fetch fetches origin with a 30-second network deadline, terminating Git and
// its transport subprocesses on expiry. A repository without origin is a no-op.
func Fetch(repoRoot string) error {
	return fetchWithTimeout(repoRoot, 30*time.Second)
}

func fetchWithTimeout(repoRoot string, timeout time.Duration) error {
	if !HasRemote(repoRoot, "origin") {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "fetch", "origin")
	cmd.Dir = repoRoot
	// Bound waiting on inherited output pipes as well as the direct process.
	cmd.WaitDelay = time.Second
	configureFetchCancellation(cmd)
	_, err := cmd.Output()
	if ctx.Err() != nil {
		return fmt.Errorf("git fetch origin timed out after %s: %w (%v)", timeout, ctx.Err(), err)
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		return fmt.Errorf("git fetch origin: %s", strings.TrimSpace(string(exitErr.Stderr)))
	}
	return err
}
