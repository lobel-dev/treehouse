package pool

import "github.com/kunchenguid/treehouse/internal/vcs"

// MergeComparison records an actual classification check, including failures.
// Reporting must not turn an unknown recorded-base result into deletion consent.
type MergeComparison struct {
	BaseBranch string
	Ref        string
	Result     string
	Detail     string
}

type RefusalFacts struct {
	Git         vcs.GitWorktreeFacts
	Comparisons []MergeComparison
}

// recordedBaseComparison gives an explicitly configured base a second reading
// only after the default comparison definitively found unmerged work. Callers
// never use it to rescue an unverifiable default or an inferred base.
func recordedBaseComparison(path, branch string) (bool, MergeComparison) {
	comparison := MergeComparison{BaseBranch: branch, Result: "unknown"}
	comparison.Ref = vcs.BaseBranchMergeRef(path, branch)
	if comparison.Ref == "" {
		return false, comparison
	}
	merged, err := vcs.IsHeadMergedIntoRef(path, comparison.Ref)
	if err != nil {
		comparison.Detail = err.Error()
		return false, comparison
	}
	comparison.Result = "not merged"
	if merged {
		comparison.Result = "merged"
	}
	return merged, comparison
}
