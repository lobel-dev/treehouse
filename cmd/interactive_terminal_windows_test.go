//go:build windows

package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/mattn/go-isatty"
	"golang.org/x/sys/windows"
)

// msysTerminalPipe uses the same pipe naming contract as MSYS/MinTTY.
func msysTerminalPipe(t *testing.T, index int) (*os.File, *os.File) {
	t.Helper()
	name := fmt.Sprintf(`\\.\pipe\msys-%x-pty%d-to-master`, time.Now().UnixNano(), index)
	ptr, err := windows.UTF16PtrFromString(name)
	if err != nil {
		t.Fatal(err)
	}
	server, err := windows.CreateNamedPipe(ptr, windows.PIPE_ACCESS_DUPLEX, windows.PIPE_TYPE_BYTE|windows.PIPE_WAIT, 1, 65536, 65536, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	master := os.NewFile(uintptr(server), name)
	t.Cleanup(func() { master.Close() })
	client, err := windows.CreateFile(ptr, windows.GENERIC_READ|windows.GENERIC_WRITE, 0, nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	slave := os.NewFile(uintptr(client), name)
	t.Cleanup(func() { slave.Close() })
	if err := windows.ConnectNamedPipe(server, nil); err != nil && err != windows.ERROR_PIPE_CONNECTED {
		t.Fatal(err)
	}
	if isatty.IsTerminal(slave.Fd()) || !isatty.IsCygwinTerminal(slave.Fd()) {
		t.Fatal("fixture must be an MSYS pipe, not a native console")
	}
	return master, slave
}

func TestInteractiveMSYSTerminalE2E(t *testing.T) {
	for _, mode := range []string{"home", "prune", "redirect-0", "redirect-1", "redirect-2"} {
		t.Run(mode, func(t *testing.T) {
			repo, home := setupTestRepo(t)
			var args []string
			input := "q\n"
			if mode == "prune" {
				idleReportingSlot(t, repo, home)
				args = []string{"prune"}
				input = "n\n"
			}
			var master, slave [3]*os.File
			for i := range master {
				master[i], slave[i] = msysTerminalPipe(t, i)
			}
			if _, err := master[0].WriteString(input); err != nil {
				t.Fatal(err)
			}
			var redirected *os.File
			for i := range slave {
				if mode == fmt.Sprintf("redirect-%d", i) {
					var err error
					redirected, err = os.CreateTemp(t.TempDir(), "redirect")
					if err != nil {
						t.Fatal(err)
					}
					defer redirected.Close()
					if i == 0 {
						redirected.WriteString(input)
						redirected.Seek(0, io.SeekStart)
					}
					slave[i].Close()
					slave[i] = redirected
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			child := exec.CommandContext(ctx, treehouseBin, args...)
			child.Dir = repo
			child.Env = buildEnv(home, "SHELL="+exitShellBin)
			child.Stdin, child.Stdout, child.Stderr = slave[0], slave[1], slave[2]
			err := child.Run()
			var output strings.Builder
			for i, file := range slave {
				if file == redirected {
					file.Seek(0, io.SeekStart)
					data, readErr := io.ReadAll(file)
					if readErr != nil {
						t.Fatal(readErr)
					}
					output.Write(data)
				} else {
					file.Close()
					if i > 0 {
						data, readErr := io.ReadAll(master[i])
						if readErr != nil {
							t.Fatal(readErr)
						}
						output.Write(data)
					}
				}
			}
			if err != nil {
				t.Fatalf("CLI: %v\n%s", err, output.String())
			}
			text := output.String()
			if mode == "home" && !strings.Contains(text, "Treehouse ·") {
				t.Fatalf("MSYS home not dispatched: %s", text)
			}
			if mode == "prune" && !strings.Contains(text, "Remove the listed trees?") {
				t.Fatalf("MSYS prune not interactive: %s", text)
			}
			if strings.HasPrefix(mode, "redirect-") && strings.Contains(text, "Treehouse ·") {
				t.Fatalf("redirect opened home: %s", text)
			}
		})
	}
}
