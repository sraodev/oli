package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/sraodev/mac-cleanup-studio/internal/cleanup"
	"github.com/sraodev/mac-cleanup-studio/internal/cli"
)

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
