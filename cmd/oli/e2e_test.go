package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/sraodev/oli/internal/cleanup"
)

func TestOliBinaryE2E(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("cleanup executable supports macOS only")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	binary := filepath.Join(t.TempDir(), "oli")
	if output, err := exec.CommandContext(ctx, "go", "build", "-o", binary, ".").CombinedOutput(); err != nil {
		t.Fatalf("build Oli: %v\n%s", err, output)
	}
	fixtureHome := t.TempDir()
	candidate := filepath.Join(fixtureHome, "Library", "Caches", "oli-e2e", "data")
	if err := os.MkdirAll(filepath.Dir(candidate), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(candidate, []byte("synthetic cache"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-40 * 24 * time.Hour)
	for _, path := range []string{candidate, filepath.Dir(candidate)} {
		if err := os.Chtimes(path, old, old); err != nil {
			t.Fatal(err)
		}
	}
	runBinary := func(wantCode int, args ...string) []byte {
		t.Helper()
		command := exec.CommandContext(ctx, binary, args...)
		// Only the child sees a synthetic home; never scan the developer's files.
		command.Env = append(os.Environ(), "HOME="+fixtureHome)
		output, err := command.CombinedOutput()
		code := 0
		if err != nil {
			if exit, ok := err.(*exec.ExitError); ok {
				code = exit.ExitCode()
			} else {
				t.Fatalf("oli %v: %v", args, err)
			}
		}
		if code != wantCode {
			t.Fatalf("oli %v: exit %d, want %d\n%s", args, code, wantCode, output)
		}
		return output
	}
	for _, test := range []struct {
		command, want string
	}{
		{"help", "Oli — Open Lifecycle Intelligence"},
		{"version", "oli dev (commit unknown, built unknown)"},
	} {
		if output := runBinary(0, test.command); !strings.Contains(string(output), test.want) {
			t.Fatalf("%s branding: %s", test.command, output)
		}
	}
	var capabilities struct {
		Product       string `json:"product"`
		SchemaVersion string `json:"schema_version"`
	}
	if err := json.Unmarshal(runBinary(0, "capabilities", "--json"), &capabilities); err != nil {
		t.Fatal(err)
	}
	if capabilities.Product != "oli" || capabilities.SchemaVersion != "mac-cleanup-studio/v1" {
		t.Fatalf("capabilities: %+v", capabilities)
	}
	for _, command := range []string{"scan", "recommend", "clean"} {
		output := runBinary(0, command, "--rules", "user-caches", "--json")
		var report struct {
			Scan            cleanup.Scan `json:"scan"`
			Recommendations []struct {
				Detected cleanup.Metrics `json:"detected"`
			} `json:"recommendations"`
		}
		if err := json.Unmarshal(output, &report); err != nil {
			t.Fatalf("%s returned invalid JSON: %v", command, err)
		}
		detected := report.Scan.Detected.LogicalBytes
		if command == "recommend" && len(report.Recommendations) == 1 {
			detected = report.Recommendations[0].Detected.LogicalBytes
		}
		if detected != int64(len("synthetic cache")) {
			t.Fatalf("%s fixture bytes = %d; report: %s", command, detected, output)
		}
		if data, err := os.ReadFile(candidate); err != nil || string(data) != "synthetic cache" {
			t.Fatalf("%s changed a previewed file: %q, %v", command, data, err)
		}
	}
	for _, flag := range []string{"--apply", "--yes"} {
		runBinary(2, "clean", "--rules", "user-caches", flag)
		if _, err := os.Stat(candidate); err != nil {
			t.Fatalf("incomplete confirmation changed fixture: %v", err)
		}
	}
	var applied struct {
		Result cleanup.CleanResult `json:"result"`
	}
	if err := json.Unmarshal(runBinary(0, "clean", "--rules", "user-caches", "--apply", "--yes", "--json"), &applied); err != nil {
		t.Fatal(err)
	}
	if len(applied.Result.Removed) != 1 || len(applied.Result.Failures) != 0 {
		t.Fatalf("fixture cleanup result: %+v", applied.Result)
	}
	if _, err := os.Stat(candidate); !os.IsNotExist(err) {
		t.Fatalf("confirmed fixture cleanup: %v", err)
	}

	// The expanded catalog must preserve app data and keep report-only caches
	// outside deletion authority, including through the executable interface.
	log := filepath.Join(fixtureHome, ".cacher", "logs", "old.log")
	snippet := filepath.Join(fixtureHome, ".cacher", "snippets", "work")
	media := filepath.Join(fixtureHome, "Library", "Application Support", "Adobe", "Common", "Media Cache Files", "sample")
	for _, path := range []string{log, snippet, media} {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("fixture"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, old, old); err != nil {
			t.Fatal(err)
		}
	}
	runBinary(0, "scan", "--rules", "app-specific-caches", "--json")
	runBinary(1, "clean", "--rules", "app-specific-caches", "--apply", "--yes")
	runBinary(0, "clean", "--rules", "app-specific-logs", "--json")
	if _, err := os.Stat(log); err != nil {
		t.Fatal("preview deleted app log")
	}
	runBinary(2, "clean", "--rules", "app-specific-logs", "--apply")
	runBinary(0, "clean", "--rules", "app-specific-logs", "--apply", "--yes", "--json")
	if _, err := os.Stat(log); !os.IsNotExist(err) {
		t.Fatal("confirmed cleanup retained app log")
	}
	for _, path := range []string{snippet, media} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("protected file changed: %s: %v", path, err)
		}
	}
}
