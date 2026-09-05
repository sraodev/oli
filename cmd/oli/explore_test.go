package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sraodev/oli/internal/cleanup"
	"github.com/sraodev/oli/internal/cli"
)

func TestRunExplorePartialReportAndCLIOptions(t *testing.T) {
	home := t.TempDir()
	if err := os.Symlink(t.TempDir(), filepath.Join(home, "Downloads")); err != nil {
		t.Fatal(err)
	}
	engine, err := cleanup.NewDefault(home)
	if err != nil {
		t.Fatal(err)
	}
	for _, jsonOutput := range []bool{false, true} {
		args := []string{"explore", "--scope", "downloads", "--min-size-mib", "250", "--older-than-days", "365", "--limit", "2"}
		if jsonOutput {
			args = append(args, "--json")
		}
		opts, err := cli.Parse(args)
		if err != nil {
			t.Fatal(err)
		}
		var output bytes.Buffer
		if err := runExplore(context.Background(), opts, engine, &output); err != nil {
			t.Fatal(err)
		}
		if jsonOutput {
			var result struct {
				Report cleanup.Exploration `json:"report"`
			}
			if err := json.Unmarshal(output.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if !result.Report.Partial || result.Report.MinSizeBytes != 250<<20 || result.Report.OlderDays != 365 || result.Report.Limit != 2 || len(result.Report.Warnings) != 1 {
				t.Fatalf("report=%+v", result.Report)
			}
		} else {
			for _, want := range []string{"read-only", "Warning:", "This report is partial.", "not reclaimable space", "not last use"} {
				if !strings.Contains(output.String(), want) {
					t.Fatalf("missing %q in %s", want, output.String())
				}
			}
		}
	}
}

func TestRunExploreCancellationAndExpiredDeadlineEmitNoReport(t *testing.T) {
	engine, err := cleanup.NewDefault(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	expired, stop := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer stop()
	for _, test := range []struct {
		ctx  context.Context
		want error
	}{{cancelled, context.Canceled}, {expired, context.DeadlineExceeded}} {
		var output bytes.Buffer
		err := runExplore(test.ctx, cli.Options{Scope: "downloads", JSON: true}, engine, &output)
		if !errors.Is(err, test.want) || output.Len() != 0 {
			t.Fatalf("error=%v output=%q", err, output.String())
		}
	}
}

func TestRunExploreJSONHasVersionAndNoDeletionPlan(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "Downloads")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "keep.txt")
	if err := os.WriteFile(path, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	engine, err := cleanup.NewDefault(home)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := runExplore(context.Background(), cli.Options{Scope: "downloads", JSON: true}, engine, &output); err != nil {
		t.Fatal(err)
	}
	var result struct {
		SchemaVersion string              `json:"schema_version"`
		Mode          string              `json:"mode"`
		Report        cleanup.Exploration `json:"report"`
	}
	if err := json.Unmarshal(output.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.SchemaVersion != schemaVersion || result.Mode != "explore" || result.Report.Action != cleanup.ActionScanOnly || result.Report.Total.UniqueFiles != 1 {
		t.Fatalf("unexpected output: %s", output.String())
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("explore mutated a file", err)
	}
}
