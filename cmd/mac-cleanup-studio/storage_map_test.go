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

func TestStorageMapCLIJSONEndToEnd(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "Downloads", "nested", "keep.pdf")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("not opened"), 0600); err != nil {
		t.Fatal(err)
	}
	e, err := cleanup.NewDefault(home)
	if err != nil {
		t.Fatal(err)
	}
	opts, err := cli.Parse([]string{"explore", "--map", "--extension", "pdf", "--kind", "file", "--search", "KEEP", "--json"})
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := runExplore(context.Background(), opts, e, &out); err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Schema string              `json:"schema_version"`
		Report cleanup.Exploration `json:"report"`
		View   []cleanup.MapNode   `json:"map_view"`
	}
	if err := json.Unmarshal(out.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Schema != schemaVersion || len(doc.View) != 1 || doc.View[0].Metrics.LogicalBytes != 10 || doc.Report.Action != cleanup.ActionScanOnly {
		t.Fatalf("doc=%+v", doc)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "not opened" {
		t.Fatal("file changed")
	}
	opts.Search = "no-such-file"
	out.Reset()
	if err := runExplore(context.Background(), opts, e, &out); err != nil {
		t.Fatal(err)
	}
	var empty map[string]json.RawMessage
	if err := json.Unmarshal(out.Bytes(), &empty); err != nil || string(empty["map_view"]) != "[]" {
		t.Fatalf("empty map_view must be an array: %s (%v)", out.String(), err)
	}
	for _, args := range [][]string{{"explore", "--map-min-mib", "-1"}, {"explore", "--kind", "executable"}, {"explore", "--extension", "../pdf"}, {"explore", "--modified-before", "yesterday"}, {"explore", "--modified-after", "2026-01-02", "--modified-before", "2026-01-01"}} {
		if _, err := cli.Parse(args); err == nil {
			t.Fatalf("invalid filters accepted: %v", args)
		}
	}
}
