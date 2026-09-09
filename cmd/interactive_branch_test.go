package cmd

import (
	"path/filepath"
	"testing"

	"github.com/kunchenguid/treehouse/internal/pool"
	"github.com/kunchenguid/treehouse/internal/vcs"
	"github.com/kunchenguid/treehouse/internal/vcs/gitvcs"
)

func TestWorkBranchLabel(t *testing.T) {
	path := t.TempDir()
	for _, status := range []string{pool.StatusAvailable, pool.StatusDirty, pool.StatusLeased, pool.StatusInUse, pool.StatusHere, pool.StatusDamaged} {
		t.Run(status, func(t *testing.T) {
			branch := vcs.GitWorkBranch{Name: "feature/held", Holders: []gitvcs.BranchHolder{{Path: path}}}
			label, eligible := workBranchLabel(branch, []pool.WorktreeStatus{{Name: "1", Path: path, Status: status}})
			wantEligible := status == pool.StatusAvailable || status == pool.StatusDirty
			wantLabel := "feature/held · tree 1"
			if status == pool.StatusDirty {
				wantLabel += " · uncommitted changes"
			}
			if eligible != wantEligible || label != wantLabel {
				t.Fatalf("label=%q eligible=%v", label, eligible)
			}
		})
	}
	for _, tc := range []struct {
		name     string
		holders  []gitvcs.BranchHolder
		eligible bool
	}{
		{"unheld", nil, true},
		{"outside", []gitvcs.BranchHolder{{Path: t.TempDir()}}, false},
		{"missing", []gitvcs.BranchHolder{{Path: filepath.Join(path, "missing")}}, false},
		{"multiple", []gitvcs.BranchHolder{{Path: path}, {Path: path}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, eligible := workBranchLabel(vcs.GitWorkBranch{Name: "feature/test", Holders: tc.holders}, []pool.WorktreeStatus{{Name: "1", Path: path, Status: pool.StatusAvailable}})
			if eligible != tc.eligible {
				t.Fatalf("eligible=%v want=%v", eligible, tc.eligible)
			}
		})
	}
}
