package updater

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

type releaseTransport func(*http.Request) (*http.Response, error)

func (f releaseTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestBackgroundCheckUsesForkRelease(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", home)
	}
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })
	var requested string
	http.DefaultTransport = releaseTransport(func(r *http.Request) (*http.Response, error) {
		requested = r.URL.String()
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"tag_name":"v99.0.0","assets":[]}`)), Header: make(http.Header)}, nil
	})
	RunBackgroundCheck([]string{"v1.0.0"})
	if requested != "https://api.github.com/repos/lobel-dev/treehouse/releases/latest" {
		t.Errorf("background updater requested %q", requested)
	}
	if got := ReadCache("v1.0.0"); got == nil || got.LatestVersion != "v99.0.0" {
		t.Fatalf("fork release cache = %+v", got)
	}
	if IsCacheStale("v1.0.0") {
		t.Error("new fork cache should be fresh")
	}
}

func TestCacheRejectsOtherReleaseSources(t *testing.T) {
	for _, source := range []string{"", "https://api.github.com/repos/kunchenguid/treehouse/releases/latest"} {
		t.Run(source, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			if runtime.GOOS == "windows" {
				t.Setenv("USERPROFILE", home)
			}
			data, err := json.Marshal(map[string]any{"checked_at": time.Now(), "latest_version": "v99.0.0", "source": source})
			if err != nil {
				t.Fatal(err)
			}
			if err = os.MkdirAll(filepath.Join(home, treehouseDir), 0o755); err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(cachePath(), data, 0o644); err != nil {
				t.Fatal(err)
			}
			if got := ReadCache("v1.0.0"); got != nil {
				t.Errorf("foreign cache advertised update: %+v", got)
			}
			if !IsCacheStale("v1.0.0") {
				t.Error("foreign cache suppressed fork release check")
			}
		})
	}
}

// Stop at the archive request so this exercises the shipped installer without
// downloading a binary, installing anything, or accessing the network.
func TestShellInstallerUsesForkRelease(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX installer")
	}
	home := t.TempDir()
	bin := filepath.Join(home, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	curl := `#!/bin/sh
printf '%s\n' "$@" >> "$REQUEST_LOG"
case "$*" in
 */releases/latest*) printf '{"tag_name":"v99.0.0"}\n';;
 *) exit 22;;
esac
`
	if err := os.WriteFile(filepath.Join(bin, "curl"), []byte(curl), 0o755); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(home, "requests")
	t.Setenv("HOME", home)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("REQUEST_LOG", log)
	cmd := exec.Command("sh", filepath.Join("..", "..", "docs", "install.sh"))
	out, err := cmd.CombinedOutput()
	if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 22 {
		t.Fatalf("installer should stop before download: %v\n%s", err, out)
	}
	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	var urls []string
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "https://") {
			urls = append(urls, line)
		}
	}
	if len(urls) != 2 {
		t.Fatalf("requests = %q", urls)
	}
	if urls[0] != "https://api.github.com/repos/lobel-dev/treehouse/releases/latest" {
		t.Errorf("release request = %s", urls[0])
	}
	if !strings.HasPrefix(urls[1], "https://github.com/lobel-dev/treehouse/releases/download/v99.0.0/treehouse-v99.0.0-") {
		t.Errorf("archive request = %s", urls[1])
	}
}

func TestPowerShellInstallerUsesForkRelease(t *testing.T) {
	shell, err := exec.LookPath("pwsh")
	if err != nil && runtime.GOOS == "windows" {
		shell, err = exec.LookPath("powershell")
	}
	if err != nil {
		t.Skip("PowerShell is not installed")
	}
	home := t.TempDir()
	log := filepath.Join(home, "requests")
	installer, err := filepath.Abs(filepath.Join("..", "..", "docs", "install.ps1"))
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("REQUEST_LOG", log)
	t.Setenv("INSTALLER_SCRIPT", installer)
	script := `
$ErrorActionPreference = "Stop"
function Invoke-RestMethod { param($Uri); Add-Content -Encoding UTF8 -Path $env:REQUEST_LOG -Value $Uri; return @{tag_name="v99.0.0"} }
function Invoke-WebRequest { param($Uri, $OutFile); Add-Content -Encoding UTF8 -Path $env:REQUEST_LOG -Value $Uri; throw "fixture-stop-before-download" }
try { . $env:INSTALLER_SCRIPT; exit 1 }
catch { if ($_.Exception.Message -ne "fixture-stop-before-download") { Write-Error $_; exit 1 } }
finally { if ($tmpDir) { Remove-Item -Recurse -Force $tmpDir } }
`
	cmd := exec.Command(shell, "-NoProfile", "-NonInteractive", "-Command", script)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("installer fixture: %v\n%s", err, out)
	}
	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Fields(strings.TrimPrefix(string(data), "\ufeff"))
	if len(lines) != 2 {
		t.Fatalf("requests = %q", lines)
	}
	if lines[0] != "https://api.github.com/repos/lobel-dev/treehouse/releases/latest" {
		t.Errorf("release request = %s", lines[0])
	}
	if !strings.HasPrefix(lines[1], "https://github.com/lobel-dev/treehouse/releases/download/v99.0.0/treehouse-v99.0.0-windows-") {
		t.Errorf("archive request = %s", lines[1])
	}
}
