package cmd

import (
	"errors"
	"strings"
	"testing"

	"github.com/kunchenguid/treehouse/internal/pool"
)

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
