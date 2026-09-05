package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

const fixtureCapabilities = `{"schema_version":"mac-cleanup-studio/v1","commands":[{"name":"explore","read_only":true,"json":true}],"exploration_scopes":[{"id":"downloads"}]}`
const fixtureReport = `{"schema_version":"mac-cleanup-studio/v1","mode":"explore","report":{"action":"scan_only","scope":"downloads","partial":false,"total":{"logical_bytes":14},"warnings":[]}}`

func TestInspectContract(t *testing.T) {
	for _, tt := range []struct {
		name, capabilities, report, code, wantError string
		wantPartial, noExplore                      bool
	}{
		{name: "complete"},
		{name: "additive fields", report: strings.Replace(fixtureReport, `"mode":"explore"`, `"mode":"explore","future_field":true`, 1)},
		{name: "partial", report: strings.Replace(fixtureReport, `"partial":false`, `"partial":true`, 1), wantPartial: true},
		{name: "warnings are not complete", report: strings.Replace(fixtureReport, `"warnings":[]`, `"warnings":[{"code":"scope_unavailable","display_path":"private-fixture","message":"private-fixture"}]`, 1), wantPartial: true},
		{name: "unknown capabilities schema", capabilities: strings.Replace(fixtureCapabilities, "/v1", "/v2", 1), wantError: "unsupported Oli schema", noExplore: true},
		{name: "unsupported command", capabilities: strings.Replace(fixtureCapabilities, `"explore"`, `"other"`, 1), wantError: "unavailable", noExplore: true},
		{name: "mutating capability", capabilities: strings.Replace(fixtureCapabilities, `"read_only":true`, `"read_only":false`, 1), wantError: "unavailable", noExplore: true},
		{name: "no JSON capability", capabilities: strings.Replace(fixtureCapabilities, `"json":true`, `"json":false`, 1), wantError: "unavailable", noExplore: true},
		{name: "unsupported scope", capabilities: strings.Replace(fixtureCapabilities, `"downloads"`, `"other"`, 1), wantError: "unavailable", noExplore: true},
		{name: "malformed capabilities", capabilities: "not JSON", wantError: "invalid JSON", noExplore: true},
		{name: "runtime failure ignores valid JSON", code: "1", wantError: "exit 1"},
		{name: "usage failure ignores valid JSON", code: "2", wantError: "exit 2"},
		{name: "cancelled ignores valid JSON", code: "130", wantError: "context canceled"},
		{name: "malformed report", report: "not JSON", wantError: "invalid JSON"},
		{name: "unknown report schema", report: strings.Replace(fixtureReport, "/v1", "/v2", 1), wantError: "unsupported exploration"},
		{name: "wrong mode", report: strings.Replace(fixtureReport, `"mode":"explore"`, `"mode":"clean"`, 1), wantError: "unsupported exploration"},
		{name: "missing report", report: `{}`, wantError: "unsupported exploration"},
		{name: "missing partial", report: strings.Replace(fixtureReport, `"partial":false,`, "", 1), wantError: "unsupported exploration"},
		{name: "missing size", report: strings.Replace(fixtureReport, `"logical_bytes":14`, "", 1), wantError: "unsupported exploration"},
		{name: "negative size", report: strings.Replace(fixtureReport, `"logical_bytes":14`, `"logical_bytes":-1`, 1), wantError: "unsupported exploration"},
		{name: "wrong action", report: strings.Replace(fixtureReport, `"scan_only"`, `"clean"`, 1), wantError: "unsupported exploration"},
		{name: "wrong scope", report: strings.Replace(fixtureReport, `"downloads"`, `"documents"`, 1), wantError: "unsupported exploration"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if tt.capabilities == "" {
				tt.capabilities = fixtureCapabilities
			}
			if tt.report == "" {
				tt.report = fixtureReport
			}
			if tt.code == "" {
				tt.code = "0"
			}
			binary := fakeOli(t, tt.capabilities, tt.report, tt.code)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			var output bytes.Buffer
			err := inspect(ctx, binary, &output)
			if tt.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantError) || output.Len() != 0 {
					t.Fatalf("error=%v output=%q; want %q and no summary", err, output.String(), tt.wantError)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				var summary struct {
					Partial bool `json:"partial"`
				}
				if err := json.Unmarshal(output.Bytes(), &summary); err != nil || summary.Partial != tt.wantPartial {
					t.Fatalf("summary=%s error=%v", output.String(), err)
				}
				if strings.Contains(output.String(), "private-fixture") {
					t.Fatal("summary exposed a private path or message")
				}
			}
			if tt.noExplore {
				if _, err := os.Stat(filepath.Join(filepath.Dir(binary), "explored")); !os.IsNotExist(err) {
					t.Fatal("attempted exploration without compatible capabilities")
				}
			}
		})
	}
}

func TestInspectContext(t *testing.T) {
	binary := fakeOli(t, fixtureCapabilities, fixtureReport, "0")
	writeFixture(t, filepath.Join(filepath.Dir(binary), "slow"), "")
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	deadline, stop := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer stop()
	for _, tt := range []struct {
		ctx  context.Context
		want error
	}{{cancelled, context.Canceled}, {deadline, context.DeadlineExceeded}} {
		var output bytes.Buffer
		if err := inspect(tt.ctx, binary, &output); !errors.Is(err, tt.want) || output.Len() != 0 {
			t.Fatalf("error=%v output=%q, want %v and no summary", err, output.String(), tt.want)
		}
	}
}

func TestInspectBinaryE2E(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Oli binary inspection currently supports macOS only")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	root := t.TempDir()
	oli, example := filepath.Join(root, "oli"), filepath.Join(root, "inspect")
	for _, target := range []struct{ path, pkg string }{{oli, "../../cmd/oli"}, {example, "."}} {
		if output, err := exec.CommandContext(ctx, "go", "build", "-o", target.path, target.pkg).CombinedOutput(); err != nil {
			t.Fatalf("build %s: %v\n%s", target.pkg, err, output)
		}
	}
	home := t.TempDir()
	downloads := filepath.Join(home, "Downloads")
	if err := os.Mkdir(downloads, 0o700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(downloads, "keep.txt")
	writeFixture(t, file, "synthetic data")
	// Validate the documentation's JSON directly against actual CLI output,
	// using only a synthetic home and deterministic logical bytes.
	for _, tt := range []struct{ name, block string }{{"complete", "inspection-complete"}, {"partial", "inspection-partial"}} {
		t.Run(tt.name, func(t *testing.T) {
			if tt.name == "partial" {
				if err := os.Rename(downloads, filepath.Join(home, "Saved")); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("Saved", downloads); err != nil {
					t.Fatal(err)
				}
			}
			command := exec.CommandContext(ctx, example, oli)
			command.Env = append(os.Environ(), "HOME="+home)
			output, err := command.CombinedOutput()
			if err != nil {
				t.Fatalf("example: %v\n%s", err, output)
			}
			guide, err := os.ReadFile("../../docs/agent-interface.md")
			if err != nil {
				t.Fatal(err)
			}
			_, block, found := strings.Cut(string(guide), "<!-- "+tt.block+" -->\n\n```json\n")
			want, _, closed := strings.Cut(block, "\n```")
			if !found || !closed {
				t.Fatal("missing documented fixture", tt.block)
			}
			var compact bytes.Buffer
			if err := json.Compact(&compact, []byte(want)); err != nil || strings.TrimSpace(string(output)) != compact.String() {
				t.Fatalf("output=%s documented=%s error=%v", output, want, err)
			}
			if data, err := os.ReadFile(file); err != nil || string(data) != "synthetic data" {
				t.Fatalf("inspection changed fixture: %q %v", data, err)
			}
		})
	}
	for _, tt := range []struct {
		name     string
		args     []string
		wantCode int
	}{
		{name: "usage", wantCode: 2},
		{name: "runtime", args: []string{fakeOli(t, fixtureCapabilities, fixtureReport, "1")}, wantCode: 1},
		{name: "child usage", args: []string{fakeOli(t, fixtureCapabilities, fixtureReport, "2")}, wantCode: 1},
		{name: "cancelled", args: []string{fakeOli(t, fixtureCapabilities, fixtureReport, "130")}, wantCode: 130},
	} {
		t.Run(tt.name, func(t *testing.T) {
			command := exec.CommandContext(ctx, example, tt.args...)
			command.Env = append(os.Environ(), "HOME="+home)
			output, err := command.Output()
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != tt.wantCode || len(output) != 0 {
				t.Fatalf("error=%v stdout=%q; want exit %d and no summary", err, output, tt.wantCode)
			}
		})
	}
}

func fakeOli(t *testing.T, capabilities, report, code string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture requires a Unix host")
	}
	root := t.TempDir()
	writeFixture(t, filepath.Join(root, "capabilities.json"), capabilities)
	writeFixture(t, filepath.Join(root, "report.json"), report)
	writeFixture(t, filepath.Join(root, "code"), code)
	binary := filepath.Join(root, "oli")
	writeFixture(t, binary, `#!/bin/sh
cd "$(dirname "$0")" || exit 1
case "$*" in
  "capabilities --json") cat capabilities.json ;;
  "explore --scope downloads --limit 10 --json")
    touch explored
    if [ -f slow ]; then exec sleep 30; fi
    cat report.json
    exit "$(cat code)"
    ;;
  *) exit 2 ;;
esac
`)
	if err := os.Chmod(binary, 0o700); err != nil {
		t.Fatal(err)
	}
	return binary
}

func writeFixture(t *testing.T, path, data string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
}
