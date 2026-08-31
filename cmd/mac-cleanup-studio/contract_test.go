package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sraodev/mac-cleanup-studio/internal/cleanup"
	"github.com/sraodev/mac-cleanup-studio/internal/cli"
)

func TestJSONUsageErrorIsSingleRedactedDocument(t *testing.T) {
	var out, detail bytes.Buffer
	if exit := run(context.Background(), []string{"scan", "--profile", "/Users/private/secret-token", "--json"}, &out, &detail); exit != 2 {
		t.Fatalf("exit=%d", exit)
	}
	decoder := json.NewDecoder(&out)
	var doc map[string]any
	if err := decoder.Decode(&doc); err != nil {
		t.Fatal(err)
	}
	if doc["code"] != "invalid_arguments" || doc["mode"] != "error" || doc["schema_version"] != schemaVersion {
		t.Fatalf("doc=%v", doc)
	}
	if err := decoder.Decode(&doc); err != io.EOF {
		t.Fatalf("extra output: %v", err)
	}
	if strings.Contains(doc["error"].(string), "secret-token") {
		t.Fatal("argument leaked")
	}
	if !strings.Contains(detail.String(), "secret-token") {
		t.Fatal("local diagnostic detail lost")
	}
}

func TestDiagnosticExportIsOptInAndAllowlisted(t *testing.T) {
	var out, detail bytes.Buffer
	if exit := run(context.Background(), []string{"diagnostics", "--json"}, &out, &detail); exit != 2 {
		t.Fatal("diagnostics lacked opt-in")
	}
	out.Reset()
	if exit := run(context.Background(), []string{"diagnostics", "--share", "--json"}, &out, &detail); exit != 0 {
		t.Fatalf("exit=%d %s", exit, detail.String())
	}
	var doc map[string]any
	if err := json.Unmarshal(out.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"schema_version", "mode", "product", "platform", "architecture", "go_version", "data_policy"} {
		if _, ok := doc[key]; !ok {
			t.Fatalf("missing %s", key)
		}
		delete(doc, key)
	}
	if len(doc) != 0 {
		t.Fatalf("unreviewed export fields: %v", doc)
	}
}

func TestCleanupReportsFreshProtectionAsPartialFailure(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "cache")
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "old"), []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	engine, err := cleanup.New(home, []cleanup.Rule{{ID: "cache", Name: "cache", Action: cleanup.ActionClean, Risk: cleanup.RiskSafe, Auto: true, MinimumAge: 1, Roots: []string{root}}})
	if err != nil {
		t.Fatal(err)
	}
	// Review input is a boundary between scanning and mutation; protect the
	// selected candidate at that boundary to exercise fresh validation.
	input := &protectingInput{home: home, t: t}
	var out, review bytes.Buffer
	err = runCleanupWithInput(context.Background(), cli.Options{Command: cli.CommandClean, Profile: cli.ProfileSafe, Apply: true, Yes: true, JSON: true, Interactive: true}, engine, home, &out, input, &review)
	if !errors.Is(err, cleanup.ErrPartial) {
		t.Fatalf("err=%v output=%s review=%s", err, out.String(), review.String())
	}
	var doc map[string]any
	if err := json.Unmarshal(out.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	if doc["mode"] != "partial" || doc["error_code"] != "partial_failure" {
		t.Fatalf("doc=%v", doc)
	}
	if _, err := os.Stat(filepath.Join(root, "old")); err != nil {
		t.Fatal("protected candidate removed")
	}
}

type protectingInput struct {
	home string
	t    *testing.T
	done bool
}

func (r *protectingInput) Read(p []byte) (int, error) {
	if r.done {
		return 0, io.EOF
	}
	r.done = true
	path := filepath.Join(r.home, cleanup.ProtectionConfig)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"version":1,"rules":["cache"]}`), 0600); err != nil {
		r.t.Fatal(err)
	}
	return copy(p, "1\nDELETE\n"), nil
}
