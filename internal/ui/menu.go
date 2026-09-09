package ui

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/mattn/go-isatty"
)

// IsInteractive requires all three standard streams to be native or MSYS/Cygwin
// terminals. Redirecting any stream retains non-interactive command behavior.
func IsInteractive() bool {
	for _, stream := range []*os.File{os.Stdin, os.Stdout, os.Stderr} {
		if !isatty.IsTerminal(stream.Fd()) && !isatty.IsCygwinTerminal(stream.Fd()) {
			return false
		}
	}
	return true
}

// ReadLine deliberately does not read ahead: the next input may belong to a
// child shell or to a different prompt after that shell exits.
func ReadLine(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	var value strings.Builder
	var b [1]byte
	for {
		_, err := io.ReadFull(os.Stdin, b[:])
		if err != nil {
			return value.String(), err
		}
		if b[0] == '\n' {
			return strings.TrimSuffix(value.String(), "\r"), nil
		}
		value.WriteByte(b[0])
	}
}

// Choose returns the selected index, or -1 on cancellation/EOF.
func Choose(title string, choices []string, cancel string) (int, error) {
	fmt.Fprintf(os.Stderr, "\n%s\n\n", title)
	for i, choice := range choices {
		fmt.Fprintf(os.Stderr, "  %d. %s\n", i+1, choice)
	}
	fmt.Fprintf(os.Stderr, "  q. %s\n\n", cancel)
	for {
		line, err := ReadLine("Choose a number: ")
		if err == io.EOF {
			return -1, nil
		}
		if err != nil {
			return -1, err
		}
		line = strings.TrimSpace(line)
		if strings.EqualFold(line, "q") {
			return -1, nil
		}
		n, err := strconv.Atoi(line)
		if err == nil && n >= 1 && n <= len(choices) {
			return n - 1, nil
		}
		fmt.Fprintln(os.Stderr, "Choose one of the listed numbers, or q to go back.")
	}
}
