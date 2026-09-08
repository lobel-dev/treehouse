//go:build !windows

package cmd

// invalidSelectorChars are the characters a selector component may never
// contain on this platform. The backslash is rejected everywhere because it is
// a separator on the other platform, and NUL can truncate a path; a colon is a
// legal character in a Unix directory name, so a repository such as "proj:v2"
// must stay reachable through the selector its own listing prints.
const invalidSelectorChars = "\\\x00"
