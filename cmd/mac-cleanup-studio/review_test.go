package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sraodev/mac-cleanup-studio/internal/cleanup"
	"github.com/sraodev/mac-cleanup-studio/internal/cli"
)

func TestInteractiveCleanupEndToEnd(t *testing.T) {
	for _, test := range []struct {
		name, input      string
		apply, wantError bool
	}{
		{"dry", "2\n", false, false}, {"apply", "2\nDELETE\n", true, false},
		{"empty", "\n", true, true}, {"EOF", "", true, true},
		{"path", "/tmp/data\n", true, true}, {"range", "4\n", true, true},
		{"duplicate", "2,2\nDELETE\n", true, true}, {"confirmation", "2\ndelete\n", true, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			home := t.TempDir()
			root := filepath.Join(home, "cache")
			if err := os.Mkdir(root, 0700); err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"a", "b", "c"} {
				if err := os.WriteFile(filepath.Join(root, name), []byte(name), 0600); err != nil {
					t.Fatal(err)
				}
			}
			e, err := cleanup.New(home, []cleanup.Rule{{ID: "test", Name: "Test", Risk: cleanup.RiskSafe, Action: cleanup.ActionClean, Default: true, Auto: true, MinimumAge: 1, Roots: []string{root}}})
			if err != nil {
				t.Fatal(err)
			}
			args := []string{"clean", "--interactive", "--json"}
			if test.apply {
				args = append(args, "--apply", "--yes")
			}
			opts, err := cli.Parse(args)
			if err != nil {
				t.Fatal(err)
			}
			var stdout, stderr bytes.Buffer
			err = runCleanupWithInput(context.Background(), opts, e, home, &stdout, strings.NewReader(test.input), &stderr)
			if (err != nil) != test.wantError {
				t.Fatalf("err=%v output=%s", err, stdout.String())
			}
			if !test.wantError {
				var report struct {
					Mode      string
					Selection cleanup.Selection
					Result    *cleanup.CleanResult
				}
				if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
					t.Fatalf("stdout is not one JSON document: %v", err)
				}
				if len(report.Selection.Candidates) != 1 || report.Selection.Candidates[0].DisplayPath != "~/cache/b" || report.Selection.Metrics.LogicalBytes != 1 {
					t.Fatalf("selection=%+v", report.Selection)
				}
				if !strings.Contains(stderr.String(), "risk") || !strings.Contains(stderr.String(), "modified") {
					t.Fatalf("missing review metadata: %s", stderr.String())
				}
				if test.apply && (report.Result == nil || len(report.Result.Removed) != 1 || report.Result.Freed != report.Selection.Metrics) {
					t.Fatalf("report=%+v", report)
				}
			}
			for _, name := range []string{"a", "b", "c"} {
				_, err := os.Stat(filepath.Join(root, name))
				removed := test.apply && !test.wantError && name == "b"
				if removed && !os.IsNotExist(err) || !removed && err != nil {
					t.Fatalf("%s removed=%v stat=%v", name, removed, err)
				}
			}
		})
	}
}

func TestInteractiveReviewCancellation(t *testing.T) {
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := reviewLine(ctx, bufio.NewScanner(reader)); done <- err }()
	cancel()
	select {
	case err := <-done:
		if err != context.Canceled {
			t.Fatalf("err=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("review input did not cancel")
	}
}
