package cmd

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/kunchenguid/treehouse/internal/pool"
)

func printRefusalFacts(w io.Writer, path, flavor string, facts pool.RefusalFacts, verbose bool) {
	for _, comparison := range facts.Comparisons {
		if comparison.BaseBranch != "" {
			ref := comparison.Ref
			if ref == "" {
				ref = "ref unavailable"
			}
			fmt.Fprintf(w, "        Recorded base %s (%s): %s.\n", comparison.BaseBranch, ref, comparison.Result)
		} else {
			fmt.Fprintf(w, "        Compared with %s: %s.\n", comparison.Ref, comparison.Result)
		}
		if verbose && comparison.Detail != "" {
			fmt.Fprintf(w, "        detail: %s\n", comparison.Detail)
		}
	}
	if flavor != "git" {
		return
	}
	git := facts.Git
	if !git.IdentityKnown {
		fmt.Fprintln(w, "        Git identity and commit preservation unknown; inspect with treehouse status.")
		return
	}
	identity := "detached HEAD"
	if git.Branch != "" {
		identity = "branch " + git.Branch
	}
	switch {
	case !git.PreservationKnown:
		fmt.Fprintf(w, "        %s; commit preservation unknown.\n", identity)
	case git.PreservingRef != "":
		fmt.Fprintf(w, "        %s; HEAD preserved by %s.\n", identity, git.PreservingRef)
	default:
		fmt.Fprintf(w, "        %s; HEAD has no preserving branch, tag, or remote ref.\n", identity)
		fmt.Fprintf(w, "        Preserve first: git -C %s branch <new-name> HEAD\n", quoteReturnPath(path))
	}
}

func printWritableSlotHint(w io.Writer, path, name string) {
	if path == "" || name == "" {
		return
	}
	poolDir := filepath.Dir(filepath.Dir(path))
	selector := filepath.ToSlash(filepath.Join(filepath.Base(poolDir), name))
	fmt.Fprintf(w, "        Open another writable shell: treehouse --root %s enter %s\n", quoteReturnPath(filepath.Dir(poolDir)), quoteReturnPath(selector))
}

func printPruneRemedy(w io.Writer, wt pool.PruneSkipped) {
	switch wt.Category {
	case pool.PruneSkipUncommitted, pool.PruneSkipUnmerged:
		printWritableSlotHint(w, wt.Path, wt.Name)
		if wt.Category == pool.PruneSkipUnmerged {
			fmt.Fprintln(w, "        Inspect or merge into the checked base before pruning; pushing alone does not make HEAD merged into that base.")
		}
		fmt.Fprintf(w, "        Preview explicit removal: treehouse destroy %s --include-unlanded\n", quoteReturnPath(wt.Path))
		if wt.Flavor == "git" {
			fmt.Fprintln(w, "        Destroying a slot does not itself delete its branch.")
		}
	}
}

// A new named-path command needs every risk flag, including ones the original
// invocation already supplied. NeededFlags alone describes only missing consent.
func destroyRemedyFlags(target pool.DestroyTarget) []string {
	classes := target.Classes
	if len(classes) == 0 {
		classes = []pool.DestroyClass{target.Class}
	}
	has := func(want pool.DestroyClass) bool {
		for _, class := range classes {
			if class == want {
				return true
			}
		}
		return false
	}
	var flags []string
	if has(pool.DestroyLeased) {
		flags = append(flags, pool.IncludeLeasedFlag)
	}
	if has(pool.DestroyInUse) {
		flags = append(flags, pool.IncludeInUseFlag)
	}
	if has(pool.DestroyDirty) || has(pool.DestroyUnmerged) || has(pool.DestroyUnverified) {
		flags = append(flags, pool.IncludeUnlandedFlag)
	}
	return flags
}

func printDestroyRemedy(w io.Writer, skipped pool.DestroySkip) {
	if len(skipped.NeededFlags) == 0 && skipped.NeededFlag == "" {
		return
	}
	flags := destroyRemedyFlags(skipped.Target)
	if len(flags) == 0 || skipped.Target.Path == "" {
		return
	}
	fmt.Fprintf(w, "        Preview explicit removal: treehouse destroy %s %s\n", quoteReturnPath(skipped.Target.Path), strings.Join(flags, " "))
	if skipped.Target.Flavor == "git" {
		fmt.Fprintln(w, "        Destroying a slot does not itself delete its branch.")
	}
}
