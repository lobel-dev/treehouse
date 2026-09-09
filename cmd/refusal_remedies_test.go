package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/kunchenguid/treehouse/internal/pool"
)

func TestDestroyRemedyIncludesAlreadyAuthorizedOverlappingRisks(t *testing.T) {
	var out bytes.Buffer
	printDestroySkipped(&out, []pool.DestroySkip{{
		Target: pool.DestroyTarget{
			Name: "1", Path: "home with spaces", Flavor: "git",
			Classes: []pool.DestroyClass{pool.DestroyLeased, pool.DestroyInUse, pool.DestroyDirty},
		},
		NeededFlags: []string{pool.IncludeLeasedFlag}, LeasedBulk: true,
	}})
	want := "Preview explicit removal: treehouse destroy " + quoteReturnPath("home with spaces") + " --include-leased --include-in-use --include-unlanded"
	if !strings.Contains(out.String(), want) {
		t.Fatalf("incomplete new command: %s", out.String())
	}
	if strings.Contains(out.String(), " --yes") {
		t.Fatalf("remedy skipped preview: %s", out.String())
	}
}
