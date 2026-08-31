package cleanup

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func selectionFixture(t *testing.T) (*Engine, *Scan, string) {
	t.Helper()
	home := t.TempDir()
	root := filepath.Join(home, "cache")
	for _, name := range []string{"a", "b", "c"} {
		mustWrite(t, filepath.Join(root, name), name)
	}
	engine := mustEngine(t, home, testRule("cache", root))
	scan, err := engine.Scan(context.Background(), ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return engine, scan, root
}

func TestCandidateSelectionPreservesDeselectedAndRejectsReplay(t *testing.T) {
	e, scan, root := selectionFixture(t)
	candidate := scan.Rules[0].Candidates[1]
	opts := CleanOptions{CandidateIDs: []string{candidate.ID}}
	preview, err := e.PreviewSelection(scan, opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Candidates) != 1 || preview.Metrics != candidate.Metrics {
		t.Fatalf("preview = %+v", preview)
	}
	// Neither the display report nor a detached preview is deletion authority.
	scan.ID = "tampered"
	scan.Rules[0].Candidates[1].DisplayPath = "/outside"
	preview.Candidates[0].DisplayPath = "/also-outside"
	result, err := e.Clean(context.Background(), scan, opts)
	if err != nil || len(result.Removed) != 1 || result.Freed != candidate.Metrics {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	for _, name := range []string{"a", "c"} {
		if _, err := os.Stat(filepath.Join(root, name)); err != nil {
			t.Fatalf("deselected %s: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "b")); !os.IsNotExist(err) {
		t.Fatalf("selected b remains: %v", err)
	}
	if _, err := e.Clean(context.Background(), scan, opts); err == nil {
		t.Fatal("replayed consumed plan")
	}
	if _, err := e.PreviewSelection(scan, opts); err == nil {
		t.Fatal("previewed consumed plan")
	}
}

func TestCandidateSelectionFailsClosed(t *testing.T) {
	e, scan, root := selectionFixture(t)
	id := scan.Rules[0].Candidates[0].ID
	newScan, err := e.Scan(context.Background(), ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, opts := range []CleanOptions{
		{CandidateIDs: []string{}}, {}, {CandidateIDs: []string{""}},
		{CandidateIDs: []string{id, id}}, {CandidateIDs: []string{id}, RuleIDs: []string{"cache"}},
		{CandidateIDs: []string{"unknown"}}, {CandidateIDs: []string{filepath.Join(root, "a")}},
		{CandidateIDs: []string{newScan.Rules[0].Candidates[0].ID}},
	} {
		if _, err := e.PreviewSelection(scan, opts); err == nil {
			t.Fatalf("preview accepted %+v", opts)
		}
		if _, err := e.Clean(context.Background(), scan, opts); err == nil {
			t.Fatalf("clean accepted %+v", opts)
		}
	}
	other := mustEngine(t, e.home, testRule("cache", root))
	if _, err := other.PreviewSelection(scan, CleanOptions{CandidateIDs: []string{id}}); err == nil {
		t.Fatal("accepted foreign engine")
	}
	if _, err := e.PreviewSelection(scan, CleanOptions{CandidateIDs: []string{id}}); err != nil {
		t.Fatalf("invalid requests consumed valid scan: %v", err)
	}
	for _, name := range []string{"a", "b", "c"} {
		if _, err := os.Stat(filepath.Join(root, name)); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCandidateSelectionIneligible(t *testing.T) {
	for _, reportOnly := range []bool{false, true} {
		home := t.TempDir()
		root := filepath.Join(home, "cache")
		mustWrite(t, filepath.Join(root, "fresh"), "data")
		rule := testRule("cache", root)
		if reportOnly {
			rule.Action = ActionScanOnly
		} else {
			rule.MinimumAge = time.Hour
		}
		e := mustEngine(t, home, rule)
		scan, err := e.Scan(context.Background(), ScanOptions{})
		if err != nil {
			t.Fatal(err)
		}
		id := scan.Rules[0].Candidates[0].ID
		if _, err := e.Clean(context.Background(), scan, CleanOptions{CandidateIDs: []string{id}}); err == nil {
			t.Fatal("accepted ineligible candidate")
		}
	}
}

func TestCandidateSelectionHardLinks(t *testing.T) {
	for _, both := range []bool{false, true} {
		home := t.TempDir()
		root := filepath.Join(home, "cache")
		mustWrite(t, filepath.Join(root, "a"), "shared")
		if err := os.Link(filepath.Join(root, "a"), filepath.Join(root, "b")); err != nil {
			t.Fatal(err)
		}
		e := mustEngine(t, home, testRule("cache", root))
		scan, err := e.Scan(context.Background(), ScanOptions{})
		if err != nil {
			t.Fatal(err)
		}
		ids := []string{scan.Rules[0].Candidates[0].ID}
		if both {
			ids = append(ids, scan.Rules[0].Candidates[1].ID)
		}
		opts := CleanOptions{CandidateIDs: ids}
		preview, err := e.PreviewSelection(scan, opts)
		if err != nil || preview.Metrics.LogicalBytes != 6 || preview.Metrics.UniqueFiles != 1 {
			t.Fatalf("preview=%+v err=%v", preview, err)
		}
		result, err := e.Clean(context.Background(), scan, opts)
		if err != nil || len(result.Removed) != len(ids) || result.Freed.LogicalBytes != 6 {
			t.Fatalf("result=%+v err=%v", result, err)
		}
		if !both {
			if _, err := os.Stat(filepath.Join(root, "b")); err != nil {
				t.Fatal("deselected hardlink removed")
			}
		}
	}
}

func TestCandidateSelectionChangedFileAndPartialOutcome(t *testing.T) {
	e, scan, root := selectionFixture(t)
	ids := []string{scan.Rules[0].Candidates[0].ID, scan.Rules[0].Candidates[1].ID}
	// Updating an existing file leaves the root identity intact.
	mustWrite(t, filepath.Join(root, "a"), "changed after preview")
	result, err := e.Clean(context.Background(), scan, CleanOptions{CandidateIDs: ids})
	if err != nil || len(result.Rejected) != 1 || len(result.Removed) != 1 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	for _, name := range []string{"a", "c"} {
		if _, err := os.Stat(filepath.Join(root, name)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := e.Clean(context.Background(), scan, CleanOptions{CandidateIDs: ids}); err == nil {
		t.Fatal("replayed partial outcome")
	}
}

func TestCandidateSelectionConcurrentSingleUse(t *testing.T) {
	e, scan, _ := selectionFixture(t)
	opts := CleanOptions{CandidateIDs: []string{scan.Rules[0].Candidates[0].ID}}
	var wg sync.WaitGroup
	errors := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := e.Clean(context.Background(), scan, opts); errors <- err }()
	}
	wg.Wait()
	close(errors)
	success := 0
	for err := range errors {
		if err == nil {
			success++
		}
	}
	if success != 1 {
		t.Fatalf("successful concurrent executions=%d", success)
	}
}

func TestCandidateSelectionCallbackCanInspectConsumedPlan(t *testing.T) {
	e, scan, _ := selectionFixture(t)
	opts := CleanOptions{CandidateIDs: []string{scan.Rules[0].Candidates[0].ID}}
	opts.Callback = func(event Event) {
		if event.Kind == EventCleanStarted {
			if _, err := e.PreviewSelection(scan, opts); err == nil {
				t.Error("callback preview accepted consumed plan")
			}
			scan.ReleaseScanOnlyManifests()
		}
	}
	if _, err := e.Clean(context.Background(), scan, opts); err != nil {
		t.Fatal(err)
	}
}

func TestCandidateSelectionPartialFailure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("requires non-root permission enforcement")
	}
	home := t.TempDir()
	root := filepath.Join(home, "cache")
	blocked := filepath.Join(root, "a")
	mustWrite(t, filepath.Join(blocked, "data"), "blocked")
	mustWrite(t, filepath.Join(root, "b"), "removable")
	mustWrite(t, filepath.Join(root, "c"), "deselected")
	if err := os.Chmod(blocked, 0555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(blocked, 0755) })
	e := mustEngine(t, home, testRule("cache", root))
	scan, err := e.Scan(context.Background(), ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	opts := CleanOptions{CandidateIDs: []string{scan.Rules[0].Candidates[0].ID, scan.Rules[0].Candidates[1].ID}}
	result, err := e.Clean(context.Background(), scan, opts)
	if err != nil || len(result.Failures) != 1 || len(result.Removed) != 1 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	for _, path := range []string{filepath.Join(blocked, "data"), filepath.Join(root, "c")} {
		if _, err := os.Stat(path); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := e.Clean(context.Background(), scan, opts); err == nil {
		t.Fatal("replayed failed plan")
	}
}

func TestCandidateSelectionCancellationBeforeMutation(t *testing.T) {
	e, scan, root := selectionFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	opts := CleanOptions{CandidateIDs: []string{scan.Rules[0].Candidates[0].ID}, Callback: func(event Event) {
		if event.Kind == EventCleanStarted {
			cancel()
		}
	}}
	result, err := e.Clean(ctx, scan, opts)
	if err != context.Canceled || result == nil || len(result.Removed) != 0 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if _, err := os.Stat(filepath.Join(root, "a")); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Clean(context.Background(), scan, opts); err == nil {
		t.Fatal("replayed cancelled attempt")
	}
}
