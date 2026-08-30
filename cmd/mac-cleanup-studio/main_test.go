package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sraodev/mac-cleanup-studio/internal/cleanup"
	"github.com/sraodev/mac-cleanup-studio/internal/cli"
)

func TestRunHelpAndVersionDoNotInitializeFilesystem(t *testing.T) {
	for _, test := range []struct {
		args []string
		want string
	}{
		{args: nil, want: "preview-first macOS cleanup"},
		{args: []string{"help"}, want: "preview-first macOS cleanup"},
		{args: []string{"version"}, want: "mac-cleanup-studio"},
	} {
		var stdout, stderr bytes.Buffer
		if code := run(context.Background(), test.args, &stdout, &stderr); code != 0 {
			t.Fatalf("run(%q) code = %d, stderr = %q", test.args, code, stderr.String())
		}
		if !strings.Contains(stdout.String(), test.want) {
			t.Fatalf("run(%q) output = %q, want %q", test.args, stdout.String(), test.want)
		}
	}
}

func TestCapabilitiesJSONPublishesSafetyContract(t *testing.T) {
	home := t.TempDir()
	engine, err := cleanup.New(home, []cleanup.Rule{{
		ID: "test", Name: "Test cache", Risk: cleanup.RiskSafe, Action: cleanup.ActionClean,
		Default: true, Auto: true, MinimumAge: 24 * time.Hour, Roots: []string{filepath.Join(home, "cache")},
	}})
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := runCapabilities(cli.Options{Command: cli.CommandCapabilities, JSON: true}, engine, &output); err != nil {
		t.Fatal(err)
	}
	var document struct {
		SchemaVersion string `json:"schema_version"`
		Rules         []struct {
			ID    string   `json:"id"`
			Roots []string `json:"roots"`
		} `json:"rules"`
		Safety struct {
			DestructiveByDefault bool `json:"destructive_by_default"`
			ArbitraryPaths       bool `json:"arbitrary_paths_accepted"`
			RequiresSudo         bool `json:"requires_sudo"`
		} `json:"safety"`
	}
	if err := json.Unmarshal(output.Bytes(), &document); err != nil {
		t.Fatal(err)
	}
	if document.SchemaVersion != schemaVersion || len(document.Rules) != 1 || document.Rules[0].ID != "test" {
		t.Fatalf("unexpected capabilities: %+v", document)
	}
	if document.Safety.DestructiveByDefault || document.Safety.ArbitraryPaths || document.Safety.RequiresSudo {
		t.Fatalf("unsafe advertised contract: %+v", document.Safety)
	}
	for _, root := range document.Rules[0].Roots {
		if strings.Contains(root, home) {
			t.Fatalf("capabilities leaked absolute home path %q", root)
		}
	}
}

func TestRecommendJSONIsReadOnlyAndSuggestsEligibleSafeRule(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "cache")
	candidate := filepath.Join(root, "old-app", "data")
	if err := os.MkdirAll(filepath.Dir(candidate), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(candidate, []byte("rebuildable"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(filepath.Join(root, "old-app"), old, old); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(candidate, old, old); err != nil {
		t.Fatal(err)
	}
	engine, err := cleanup.New(home, []cleanup.Rule{{
		ID: "test", Name: "Test cache", Risk: cleanup.RiskSafe, Action: cleanup.ActionClean,
		Default: true, Auto: true, MinimumAge: 24 * time.Hour, Roots: []string{root},
	}})
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	err = runRecommend(context.Background(), cli.Options{
		Command: cli.CommandRecommend, RuleIDs: []string{"test"}, JSON: true,
	}, engine, home, &output)
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		SchemaVersion    string   `json:"schema_version"`
		SuggestedRuleIDs []string `json:"suggested_rule_ids"`
	}
	if err := json.Unmarshal(output.Bytes(), &document); err != nil {
		t.Fatal(err)
	}
	if document.SchemaVersion != schemaVersion || len(document.SuggestedRuleIDs) != 1 || document.SuggestedRuleIDs[0] != "test" {
		t.Fatalf("unexpected recommendation: %+v", document)
	}
	if _, err := os.Stat(candidate); err != nil {
		t.Fatalf("recommendation mutated candidate: %v", err)
	}
}

func TestRunUnknownCommandFailsBeforeFilesystemWork(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run(context.Background(), []string{"erase-everything"}, &stdout, &stderr); code != 2 {
		t.Fatalf("code = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "unknown command") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestDashboardURLKeepsTokenInFragment(t *testing.T) {
	got := dashboardURL(&net.TCPAddr{IP: net.ParseIP("::1"), Port: 43123}, "secret-token")
	if got != "http://[::1]:43123/#token=secret-token" {
		t.Fatalf("dashboardURL = %q", got)
	}
}

func TestSessionTokenIsRandomURLSafeValue(t *testing.T) {
	first, err := sessionToken()
	if err != nil {
		t.Fatal(err)
	}
	second, err := sessionToken()
	if err != nil {
		t.Fatal(err)
	}
	if len(first) < 32 || first == second || strings.ContainsAny(first, "+/=") {
		t.Fatalf("unexpected tokens %q and %q", first, second)
	}
}

func TestAppliedCleanupPrintsFreshPlanBeforeMutationAndReportsInterruption(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "cache")
	candidate := filepath.Join(root, "app", "data")
	if err := os.MkdirAll(filepath.Dir(candidate), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(candidate, []byte("keep until cleanup starts"), 0o644); err != nil {
		t.Fatal(err)
	}
	engine, err := cleanup.New(home, []cleanup.Rule{{
		ID: "test", Name: "Test cache", Risk: cleanup.RiskSafe,
		Action: cleanup.ActionClean, Default: true, Roots: []string{root},
	}})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	output := &cancelOnTextWriter{cancel: cancel, text: "Permanent deletion is starting"}
	err = runCleanup(ctx, cli.Options{
		Command: cli.CommandClean, RuleIDs: []string{"test"}, Apply: true, Yes: true,
	}, engine, home, output)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("runCleanup error = %v, want context cancellation", err)
	}
	if _, err := os.Stat(candidate); err != nil {
		t.Fatalf("candidate was mutated before the printed plan boundary: %v", err)
	}
	got := output.String()
	for _, want := range []string{
		"Cleanup plan authorized for this run",
		"~/cache/app",
		"Removed candidates: 0",
		"The result above may be partial",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("output missing %q:\n%s", want, got)
		}
	}
}

type cancelOnTextWriter struct {
	bytes.Buffer
	cancel context.CancelFunc
	text   string
}

func (w *cancelOnTextWriter) Write(p []byte) (int, error) {
	n, err := w.Buffer.Write(p)
	if w.cancel != nil && strings.Contains(w.Buffer.String(), w.text) {
		w.cancel()
		w.cancel = nil
	}
	return n, err
}
