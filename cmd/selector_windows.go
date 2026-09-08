//go:build windows

package cmd

// invalidSelectorChars are the characters a selector component may never
// contain on this platform. A colon carries drive and alternate-data-stream
// syntax on Windows, so it stays rejected here.
const invalidSelectorChars = "\\:\x00"
