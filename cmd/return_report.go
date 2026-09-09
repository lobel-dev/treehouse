package cmd

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/kunchenguid/treehouse/internal/pool"
	"github.com/kunchenguid/treehouse/internal/vcs"
)

func printReleaseReport(report pool.ReleaseReport, forced, confirmed bool) {
	name := report.Name
	if report.Damaged {
		fmt.Fprintf(os.Stderr, "🌳 Slot %s reservation cleared; damaged slot was not reset.\n", name)
		return
	}
	if !report.Parked {
		return
	}
	if report.PriorHead == "" {
		fmt.Fprintf(os.Stderr, "🌳 Slot %s returned to pool.\n", name)
		return
	}
	fmt.Fprintf(os.Stderr, "🌳 Slot %s parked, reset to %s.\n", name, report.TargetBranch)
	head := report.PriorHead
	if len(head) > 12 {
		head = head[:12]
	}
	ref := report.PreservingRef
	if report.AttachedBranch != "" {
		ref = report.AttachedBranch
	}
	detail := head
	if report.Subject != "" {
		detail = escapeReturnSubject(report.Subject) + "; " + head
	}
	fmt.Fprintf(os.Stderr, "Kept: %s (%s)\n", ref, detail)
	if report.ChangesKnown && report.TrackedPaths+report.UntrackedPaths > 0 {
		mode := "cleanup"
		if confirmed {
			mode = "confirmed cleanup"
		}
		if forced {
			mode = "forced cleanup"
		}
		fmt.Fprintf(os.Stderr, "Changes observed before %s: %d tracked paths, %d untracked paths.\n", mode, report.TrackedPaths, report.UntrackedPaths)
	}
	if report.AttachedBranch != "" {
		command := "treehouse"
		if rootFlag != "" {
			command += " --root " + quoteReturnPath(rootFlag)
		}
		if strings.HasPrefix(report.AttachedBranch, "-") {
			// Git plumbing can create refs that the literal work command rejects.
			fmt.Fprintf(os.Stderr, "\nResume: %s get, then git switch -- %s\n", command, quoteReturnPath(report.AttachedBranch))
		} else {
			fmt.Fprintf(os.Stderr, "\nResume: %s work %s\n", command, quoteReturnPath(report.AttachedBranch))
		}
	}
}

func escapeReturnSubject(subject string) string {
	var escaped strings.Builder
	for _, r := range subject {
		if strconv.IsPrint(r) {
			escaped.WriteRune(r)
		} else {
			quoted := strconv.QuoteRune(r)
			escaped.WriteString(quoted[1 : len(quoted)-1])
		}
	}
	return escaped.String()
}

func returnErrorRemedy(path string, err error) error {
	var unpreserved *vcs.UnpreservedHeadError
	if errors.As(err, &unpreserved) {
		return fmt.Errorf("%w; preserve it first: git -C %s branch <new-name> HEAD", err, quoteReturnPath(path))
	}
	return err
}
