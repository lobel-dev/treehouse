package pool

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func historySlot(t *testing.T) (repo, dir, path string) {
	t.Helper()
	repo, dir = setupRepo(t)
	path, err := Acquire(repo, dir, 2, nil)
	if err != nil {
		t.Fatal(err)
	}
	runGit(t, path, "switch", "-c", "feature")
	runGit(t, path, "commit", "--allow-empty", "-m", "saved")
	if err := Release(dir, path); err != nil {
		t.Fatal(err)
	}
	entry, err := FindByPath(dir, path)
	if err != nil || entry == nil || entry.LastBranch != "feature" {
		t.Fatalf("missing locked release history: %+v %v", entry, err)
	}
	return
}

func TestHistoryClearsOnAcquisitionAndDetachedReturn(t *testing.T) {
	for _, acquire := range []bool{false, true} {
		t.Run(map[bool]string{false: "return", true: "acquire"}[acquire], func(t *testing.T) {
			repo, dir, path := historySlot(t)
			if acquire {
				got, err := Acquire(repo, dir, 2, nil)
				if err != nil || got != path {
					t.Fatalf("history prevented reuse: %s %v", got, err)
				}
			} else if err := Release(dir, path); err != nil {
				t.Fatal(err)
			}
			entry, err := FindByPath(dir, path)
			if err != nil || entry.LastBranch != "" {
				t.Fatalf("history not cleared: %+v %v", entry, err)
			}
		})
	}
}

func TestHistoryAbsenceRequiresProofAndSnapshotDoesNotHeal(t *testing.T) {
	for _, unavailable := range []bool{false, true} {
		t.Run(map[bool]string{false: "deleted", true: "unavailable"}[unavailable], func(t *testing.T) {
			repo, dir, path := historySlot(t)
			if unavailable {
				if err := os.WriteFile(filepath.Join(path, ".git"), []byte("invalid\n"), 0600); err != nil {
					t.Fatal(err)
				}
			} else {
				runGit(t, repo, "branch", "-D", "feature")
			}
			before, err := os.ReadFile(stateFilePath(dir))
			if err != nil {
				t.Fatal(err)
			}
			rows, err := ListSnapshot(dir)
			if err != nil || len(rows) != 1 {
				t.Fatalf("snapshot: %+v %v", rows, err)
			}
			want := ""
			if unavailable {
				want = "feature"
			}
			if rows[0].LastBranch != want {
				t.Fatalf("incorrect visible history: %+v", rows[0])
			}
			after, err := os.ReadFile(stateFilePath(dir))
			if err != nil || !bytes.Equal(before, after) {
				t.Fatalf("snapshot wrote state: %v", err)
			}
			if _, err := List(dir); err != nil {
				t.Fatal(err)
			}
			entry, err := FindByPath(dir, path)
			if err != nil || entry.LastBranch != want {
				t.Fatalf("incorrect history healing: %+v %v", entry, err)
			}
		})
	}
}

func TestDuplicateHistoryRemainsAdvisory(t *testing.T) {
	repo, dir, first := historySlot(t)
	if _, err := LeaseExisting(dir, "1", "keep-first"); err != nil {
		t.Fatal(err)
	}
	second, err := Acquire(repo, dir, 2, nil)
	if err != nil || first == second {
		t.Fatalf("lease ignored: %s %v", second, err)
	}
	runGit(t, second, "switch", "feature")
	if err := Release(dir, second); err != nil {
		t.Fatal(err)
	}
	rows, err := ListSnapshot(dir)
	if err != nil || len(rows) != 2 {
		t.Fatalf("snapshot: %+v %v", rows, err)
	}
	for _, row := range rows {
		if row.LastBranch != "feature" {
			t.Fatalf("history treated as exclusive ownership: %+v", rows)
		}
	}
	if rows[0].Status != StatusLeased {
		t.Fatalf("history masked lease: %+v", rows)
	}
}
