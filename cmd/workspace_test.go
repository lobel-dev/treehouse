package cmd

import (
	"errors"
	"strings"
	"testing"

	"github.com/kunchenguid/treehouse/internal/pool"
	"github.com/kunchenguid/treehouse/internal/ui"
)

func TestWorkspaceStandalonePruneFailurePrintsOnce(t *testing.T) {
	options := ui.DashboardOptions{Page: ui.CleanupPage}
	stderr, err, done := applyWorkspaceCleanup(&options, pool.PruneResult{}, 3, errors.New("boom"))
	if !done {
		t.Fatal("standalone failure continued the occupancy loop")
	}
	if stderr != "" {
		t.Fatalf("standalone failure printed and returned err: %q", stderr)
	}
	if err == nil || err.Error() != "Cleanup failed: boom" {
		t.Fatalf("err=%v", err)
	}
}

func TestWorkspaceEmbeddedPrunePrintsSkipReasons(t *testing.T) {
	options := ui.DashboardOptions{Page: ui.HomePage}
	result := pool.PruneResult{
		Pruned: []pool.PruneWorktree{{Name: "1"}},
		Skipped: []pool.PruneSkipped{
			{Name: "2", Reason: "in use"},
			{Name: "3", Reason: "leased"},
		},
	}
	stderr, err, done := applyWorkspaceCleanup(&options, result, 3, nil)
	if err != nil || done {
		t.Fatalf("embedded prune returned err=%v done=%v", err, done)
	}
	if !strings.Contains(stderr, "Kept tree") {
		t.Fatalf("skip reasons missing: %q", stderr)
	}
	if options.Banner != "Removed 1 unused trees." {
		t.Fatalf("banner=%q", options.Banner)
	}
	if options.BannerWarning || options.Page != ui.HomePage {
		t.Fatalf("warning=%v page=%v", options.BannerWarning, options.Page)
	}
}

func TestCleanupResultText(t *testing.T) {
	result := pool.PruneResult{
		Pruned:  []pool.PruneWorktree{{Name: "1"}},
		Skipped: []pool.PruneSkipped{{Name: "2", Reason: "in use"}},
	}
	got := cleanupResultText(result, 2, nil)
	if !strings.Contains(got, "Removed 1 unused trees.") {
		t.Fatalf("success omitted count: %q", got)
	}
	if !strings.Contains(got, "Trees that could not be safely removed were kept.") {
		t.Fatalf("success omitted keep notice: %q", got)
	}
	if !strings.Contains(got, "Kept tree 2 — in use.") {
		t.Fatalf("success omitted skip: %q", got)
	}

	got = cleanupResultText(pool.PruneResult{}, 3, errors.New("boom"))
	if strings.Contains(got, "Removed") || strings.Contains(got, "kept") {
		t.Fatalf("failure still reported a removal summary: %q", got)
	}
	if got != "Cleanup failed: boom" {
		t.Fatalf("failure omitted error: %q", got)
	}
}
