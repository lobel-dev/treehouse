package pool

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestInteractivePruneRestrictsConfirmationAndRechecksSafety(t *testing.T) {
	for _, change := range []string{"clean", "dirty", "leased"} {
		t.Run(change, func(t *testing.T) {
			repo, dir := setupRepo(t)
			first, err := Acquire(repo, dir, 4, nil)
			if err != nil {
				t.Fatal(err)
			}
			second, err := Acquire(repo, dir, 4, nil)
			if err != nil {
				t.Fatal(err)
			}
			if err := Release(dir, first); err != nil {
				t.Fatal(err)
			}
			// Second tree is in use at preview, so it is outside the approved set.
			before, err := os.ReadFile(stateFilePath(dir))
			if err != nil {
				t.Fatal(err)
			}
			preview, err := PruneWithOptions(repo, dir, PruneOptions{DryRun: true, ReadOnlySnapshot: true})
			if err != nil || len(preview.Candidates) != 1 || preview.Candidates[0].Path != first {
				t.Fatalf("preview: %+v %v", preview, err)
			}
			after, err := os.ReadFile(stateFilePath(dir))
			if err != nil || !bytes.Equal(before, after) {
				t.Fatalf("preview changed state: %v", err)
			}
			// Eligibility changes while a user is considering the confirmation.
			if err := Release(dir, second); err != nil {
				t.Fatal(err)
			}
			switch change {
			case "dirty":
				if err := os.WriteFile(filepath.Join(first, "new-work"), []byte("keep"), 0600); err != nil {
					t.Fatal(err)
				}
			case "leased":
				state, err := ReadState(dir)
				if err != nil {
					t.Fatal(err)
				}
				for i := range state.Worktrees {
					if state.Worktrees[i].Path == first {
						state.Worktrees[i].Leased = true
					}
				}
				if err := WriteState(dir, state); err != nil {
					t.Fatal(err)
				}
			}
			result, err := PruneWithOptions(repo, dir, PruneOptions{CandidatePaths: []string{first}})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(second); err != nil {
				t.Fatalf("unapproved tree removed: %v", err)
			}
			_, err = os.Stat(first)
			if change == "clean" {
				if !os.IsNotExist(err) || len(result.Pruned) != 1 {
					t.Fatalf("approved safe tree not removed: %+v %v", result, err)
				}
			} else if err != nil || len(result.Pruned) != 0 {
				t.Fatalf("newly protected tree removed: %+v %v", result, err)
			}
		})
	}
}
