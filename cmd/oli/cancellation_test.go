package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestCancelledInspectionHasNoSuccessOutput(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("CLI inspection currently supports macOS only")
	}
	home := t.TempDir()
	path := filepath.Join(home, "Library", "Caches", "keep")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	for _, command := range []string{"scan", "recommend", "clean"} {
		for _, jsonOutput := range []bool{false, true} {
			for _, expired := range []bool{false, true} {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				code, diagnostic := 130, "Cancelled."
				if expired {
					ctx, cancel = context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
					code, diagnostic = 1, "context deadline exceeded"
				}
				args := []string{command, "--rules", "user-caches"}
				if jsonOutput {
					args = append(args, "--json")
				}
				var stdout, stderr bytes.Buffer
				got := run(ctx, args, &stdout, &stderr)
				cancel()
				if got != code || stdout.Len() != 0 || !strings.Contains(stderr.String(), diagnostic) {
					t.Errorf("%v expired=%t: exit=%d stdout=%q stderr=%q; want exit %d, no report and %q", args, expired, got, stdout.String(), stderr.String(), code, diagnostic)
				}
				if data, err := os.ReadFile(path); err != nil || string(data) != "keep" {
					t.Fatalf("%v changed fixture: %q %v", args, data, err)
				}
			}
		}
	}
}
