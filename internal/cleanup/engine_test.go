package cleanup

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestNewRejectsRootsOutsideHomeAndOverlaps(t *testing.T) {
	home := t.TempDir()
	outside := t.TempDir()
	base := testRule("one", filepath.Join(home, "cache"))

	if _, err := New(home, []Rule{testRule("outside", outside)}); err == nil {
		t.Fatal("New accepted a root outside home")
	}
	if _, err := New(home, []Rule{testRule("home", home)}); err == nil {
		t.Fatal("New accepted home itself as a cleanup root")
	}
	if _, err := New(string(filepath.Separator), []Rule{testRule("system", filepath.Join(string(filepath.Separator), "tmp"))}); err == nil {
		t.Fatal("New accepted the filesystem root as home")
	}
	overlap := testRule("two", filepath.Join(home, "cache", "nested"))
	if _, err := New(home, []Rule{base, overlap}); err == nil {
		t.Fatal("New accepted overlapping roots")
	}
}

func TestScanSkipsSymlinkRootAndNeverFollowsEntrySymlink(t *testing.T) {
	home := t.TempDir()
	external := t.TempDir()
	mustWrite(t, filepath.Join(external, "keep.txt"), "outside")

	linkedRoot := filepath.Join(home, "linked-cache")
	if err := os.Symlink(external, linkedRoot); err != nil {
		t.Fatal(err)
	}
	engine, err := New(home, []Rule{testRule("linked", linkedRoot)})
	if err != nil {
		t.Fatal(err)
	}
	scan, err := engine.Scan(context.Background(), ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(scan.Rules[0].Candidates) != 0 {
		t.Fatal("symlink root produced cleanup candidates")
	}
	if len(scan.Warnings) != 1 || scan.Warnings[0].Code != "root_symlink" {
		t.Fatalf("warnings = %#v, want one root_symlink warning", scan.Warnings)
	}

	realRoot := filepath.Join(home, "real-cache")
	candidate := filepath.Join(realRoot, "app")
	if err := os.MkdirAll(candidate, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, filepath.Join(candidate, "outside-link")); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(candidate, "cache.bin"), "cache")
	engine, err = New(home, []Rule{testRule("real", realRoot)})
	if err != nil {
		t.Fatal(err)
	}
	scan, err = engine.Scan(context.Background(), ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	got := scan.Rules[0].Candidates
	if len(got) != 1 || got[0].Metrics.Files != 1 || got[0].Metrics.Symlinks != 1 {
		t.Fatalf("candidate metrics = %#v", got)
	}
	result, err := engine.Clean(context.Background(), scan, CleanOptions{RuleIDs: []string{"real"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Removed) != 1 {
		t.Fatalf("removed = %#v", result.Removed)
	}
	if _, err := os.Stat(filepath.Join(external, "keep.txt")); err != nil {
		t.Fatalf("symlink target was touched: %v", err)
	}
}

func TestRemoveCandidateCannotFollowSwappedAncestorSymlink(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "cache")
	parent := filepath.Join(root, "app", "nested")
	plannedFile := filepath.Join(parent, "cache.bin")
	mustWrite(t, plannedFile, "planned")
	external := t.TempDir()
	externalFile := filepath.Join(external, "cache.bin")
	mustWrite(t, externalFile, "outside")

	engine := mustEngine(t, home, testRule("cache", root))
	scan, err := engine.Scan(context.Background(), ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	plannedRule := scan.plan.rules["cache"]
	if plannedRule == nil || len(plannedRule.candidates) != 1 {
		t.Fatalf("unexpected frozen plan: %#v", scan.plan)
	}
	originalParent := parent + "-original"
	if err := os.Rename(parent, originalParent); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, parent); err != nil {
		t.Fatal(err)
	}

	removed, changed, err := engine.removeCandidate(
		context.Background(), plannedRule.roots[root], plannedRule.candidates[0], nil,
	)
	if err == nil || removed || !changed {
		t.Fatalf("removeCandidate = removed %v, changed %v, err %v", removed, changed, err)
	}
	if contents, err := os.ReadFile(externalFile); err != nil || string(contents) != "outside" {
		t.Fatalf("external target was touched: contents %q, err %v", contents, err)
	}
	if contents, err := os.ReadFile(filepath.Join(originalParent, "cache.bin")); err != nil || string(contents) != "planned" {
		t.Fatalf("renamed planned file was touched: contents %q, err %v", contents, err)
	}
}

func TestScanDeduplicatesHardLinkedBytes(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "cache")
	candidate := filepath.Join(root, "app")
	file := filepath.Join(candidate, "one")
	mustWrite(t, file, "123456789")
	if err := os.Link(file, filepath.Join(candidate, "two")); err != nil {
		t.Fatal(err)
	}
	engine := mustEngine(t, home, testRule("cache", root))
	scan, err := engine.Scan(context.Background(), ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	metrics := scan.Rules[0].Candidates[0].Metrics
	if metrics.Files != 2 || metrics.UniqueFiles != 1 {
		t.Fatalf("file counts = files %d, unique %d", metrics.Files, metrics.UniqueFiles)
	}
	if metrics.LogicalBytes != 9 {
		t.Fatalf("logical bytes = %d, want 9", metrics.LogicalBytes)
	}
	if scan.Detected.LogicalBytes != 9 {
		t.Fatalf("scan logical bytes = %d, want 9", scan.Detected.LogicalBytes)
	}
	result, err := engine.Clean(context.Background(), scan, CleanOptions{RuleIDs: []string{"cache"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Removed) != 1 || result.Freed.LogicalBytes != 9 {
		t.Fatalf("cleanup result = %#v", result)
	}
}

func TestCleanRemovesHardLinksAcrossCandidates(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "cache")
	first := filepath.Join(root, "a", "data")
	second := filepath.Join(root, "b", "data")
	mustWrite(t, first, "shared")
	if err := os.MkdirAll(filepath.Dir(second), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(first, second); err != nil {
		t.Fatal(err)
	}

	engine := mustEngine(t, home, testRule("cache", root))
	scan, err := engine.Scan(context.Background(), ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(scan.Rules[0].Candidates) != 2 || scan.Detected.LogicalBytes != int64(len("shared")) {
		t.Fatalf("scan = %#v", scan.Rules[0])
	}
	result, err := engine.Clean(context.Background(), scan, CleanOptions{RuleIDs: []string{"cache"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Removed) != 2 || len(result.Rejected) != 0 || len(result.Failures) != 0 {
		t.Fatalf("cleanup result = %#v", result)
	}
	for _, candidate := range []string{filepath.Dir(first), filepath.Dir(second)} {
		if _, err := os.Lstat(candidate); !os.IsNotExist(err) {
			t.Fatalf("candidate %s remains: %v", candidate, err)
		}
	}
	if result.Freed.LogicalBytes != int64(len("shared")) {
		t.Fatalf("freed logical bytes = %d, want %d", result.Freed.LogicalBytes, len("shared"))
	}
}

func TestCleanRejectsUnexpectedHardLinkChangeAcrossCandidates(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "cache")
	first := filepath.Join(root, "a", "data")
	second := filepath.Join(root, "b", "data")
	mustWrite(t, first, "shared")
	if err := os.MkdirAll(filepath.Dir(second), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(first, second); err != nil {
		t.Fatal(err)
	}

	engine := mustEngine(t, home, testRule("cache", root))
	scan, err := engine.Scan(context.Background(), ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	extraLink := filepath.Join(home, "unmanaged", "data")
	result, err := engine.Clean(context.Background(), scan, CleanOptions{
		RuleIDs: []string{"cache"},
		Callback: func(event Event) {
			if event.Kind != EventCandidateRemoved || event.DisplayPath != "~/cache/a" {
				return
			}
			if err := os.MkdirAll(filepath.Dir(extraLink), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Link(second, extraLink); err != nil {
				t.Fatal(err)
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Removed) != 1 || len(result.Rejected) != 1 {
		t.Fatalf("cleanup result = %#v", result)
	}
	if _, err := os.Lstat(second); err != nil {
		t.Fatalf("unexpectedly changed candidate was removed: %v", err)
	}
	if _, err := os.Lstat(extraLink); err != nil {
		t.Fatalf("unmanaged hard link was touched: %v", err)
	}
}

func TestAgeEligibilityUsesLatestTreeModification(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "cache")
	now := time.Now().Truncate(time.Second)
	old := now.Add(-48 * time.Hour)
	recent := now.Add(-time.Hour)

	oldCandidate := filepath.Join(root, "old-app")
	mustWrite(t, filepath.Join(oldCandidate, "cache"), "old")
	setTimes(t, filepath.Join(oldCandidate, "cache"), old)
	setTimes(t, oldCandidate, old)

	freshCandidate := filepath.Join(root, "fresh-app")
	mustWrite(t, filepath.Join(freshCandidate, "cache"), "fresh")
	setTimes(t, filepath.Join(freshCandidate, "cache"), recent)
	setTimes(t, freshCandidate, old)

	rule := testRule("aged", root)
	rule.Auto = true
	rule.Default = true
	rule.MinimumAge = 24 * time.Hour
	engine := mustEngine(t, home, rule)
	scan, err := engine.Scan(context.Background(), ScanOptions{Now: now})
	if err != nil {
		t.Fatal(err)
	}
	byName := candidatesByBase(scan.Rules[0].Candidates)
	if !byName["old-app"].Eligible || !byName["old-app"].AutoEligible {
		t.Fatalf("old candidate is not auto eligible: %#v", byName["old-app"])
	}
	if byName["fresh-app"].Eligible || byName["fresh-app"].AutoEligible {
		t.Fatalf("fresh descendant did not block eligibility: %#v", byName["fresh-app"])
	}
	result, err := engine.Clean(context.Background(), scan, CleanOptions{RuleIDs: []string{"aged"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Removed) != 1 || len(result.Skipped) != 1 {
		t.Fatalf("result = %#v", result)
	}
	if _, err := os.Stat(oldCandidate); !os.IsNotExist(err) {
		t.Fatalf("old candidate still exists: %v", err)
	}
	if _, err := os.Stat(freshCandidate); err != nil {
		t.Fatalf("fresh candidate was removed: %v", err)
	}
}

func TestCleanRejectsCandidateChangedAfterScan(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "cache")
	file := filepath.Join(root, "app", "data")
	mustWrite(t, file, "before")
	engine := mustEngine(t, home, testRule("cache", root))
	scan, err := engine.Scan(context.Background(), ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, file, "after-change")
	result, err := engine.Clean(context.Background(), scan, CleanOptions{RuleIDs: []string{"cache"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Rejected) != 1 || len(result.Removed) != 0 {
		t.Fatalf("result = %#v", result)
	}
	if _, err := os.Stat(file); err != nil {
		t.Fatalf("changed candidate was touched: %v", err)
	}
}

func TestCleanRejectsRootChangedAfterScan(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "cache")
	existing := filepath.Join(root, "existing", "data")
	mustWrite(t, existing, "keep")
	engine := mustEngine(t, home, testRule("cache", root))
	scan, err := engine.Scan(context.Background(), ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(root, "new-after-scan", "data"), "new")
	result, err := engine.Clean(context.Background(), scan, CleanOptions{RuleIDs: []string{"cache"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Rejected) != 1 || len(result.Removed) != 0 {
		t.Fatalf("result = %#v", result)
	}
	if _, err := os.Stat(existing); err != nil {
		t.Fatalf("frozen candidate was removed despite changed root: %v", err)
	}
}

func TestCleanIsScopedToSelectedRules(t *testing.T) {
	home := t.TempDir()
	rootA := filepath.Join(home, "a")
	rootB := filepath.Join(home, "b")
	pathA := filepath.Join(rootA, "candidate", "data")
	pathB := filepath.Join(rootB, "candidate", "data")
	outside := filepath.Join(home, "unmanaged", "data")
	mustWrite(t, pathA, "a")
	mustWrite(t, pathB, "b")
	mustWrite(t, outside, "outside")
	engine := mustEngine(t, home, testRule("a", rootA), testRule("b", rootB))
	scan, err := engine.Scan(context.Background(), ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	result, err := engine.Clean(context.Background(), scan, CleanOptions{RuleIDs: []string{"a"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Removed) != 1 || result.Removed[0].RuleID != "a" {
		t.Fatalf("removed = %#v", result.Removed)
	}
	if _, err := os.Stat(filepath.Dir(pathA)); !os.IsNotExist(err) {
		t.Fatalf("selected candidate remains: %v", err)
	}
	for _, path := range []string{pathB, outside} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("out-of-scope path %s was touched: %v", path, err)
		}
	}
}

func TestCleanEmitsCandidateFailed(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can remove entries from a read-only directory")
	}
	home := t.TempDir()
	root := filepath.Join(home, "cache")
	candidate := filepath.Join(root, "app")
	mustWrite(t, filepath.Join(candidate, "data"), "data")
	if err := os.Chmod(candidate, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(candidate, 0o755) })
	engine := mustEngine(t, home, testRule("cache", root))
	scan, err := engine.Scan(context.Background(), ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var failed *Event
	result, err := engine.Clean(context.Background(), scan, CleanOptions{
		RuleIDs: []string{"cache"},
		Callback: func(event Event) {
			if event.Kind == EventCandidateFailed {
				copy := event
				failed = &copy
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Failures) != 1 || failed == nil || failed.Failure == nil {
		t.Fatalf("failures = %#v, event = %#v", result.Failures, failed)
	}
	if failed.CandidateID == "" || failed.DisplayPath == "" || failed.Message == "" {
		t.Fatalf("failed event lacks context: %#v", failed)
	}
	if strings.Contains(failed.Message, home) {
		t.Fatalf("failed event leaked full home path: %q", failed.Message)
	}
}

func TestScanOnlyRulesCannotBeCleaned(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "archives")
	mustWrite(t, filepath.Join(root, "archive", "data"), "archive")
	rule := testRule("archives", root)
	rule.Risk = RiskReview
	rule.Action = ActionScanOnly
	engine := mustEngine(t, home, rule)
	scan, err := engine.Scan(context.Background(), ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = engine.Clean(context.Background(), scan, CleanOptions{RuleIDs: []string{"archives"}})
	if !errors.Is(err, ErrScanOnly) {
		t.Fatalf("Clean error = %v, want ErrScanOnly", err)
	}
	if _, err := os.Stat(filepath.Join(root, "archive", "data")); err != nil {
		t.Fatalf("scan-only data was touched: %v", err)
	}
}

func TestReleaseScanOnlyManifestsPreservesPublicResults(t *testing.T) {
	home := t.TempDir()
	cleanRoot := filepath.Join(home, "cache")
	reviewRoot := filepath.Join(home, "archives")
	mustWrite(t, filepath.Join(cleanRoot, "app", "nested", "cache.bin"), "cache")
	mustWrite(t, filepath.Join(reviewRoot, "archive", "nested", "archive.zip"), "archive")
	cleanRule := testRule("cache", cleanRoot)
	reviewRule := testRule("archives", reviewRoot)
	reviewRule.Risk = RiskReview
	reviewRule.Action = ActionScanOnly
	engine := mustEngine(t, home, cleanRule, reviewRule)

	scan, err := engine.Scan(context.Background(), ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	privateClean := scan.plan.rules[cleanRule.ID]
	privateReview := scan.plan.rules[reviewRule.ID]
	if len(privateClean.candidates) != 1 || len(privateReview.candidates) != 1 || len(privateReview.candidates[0].entries) < 2 {
		t.Fatalf("unexpected private plan before release: clean=%#v review=%#v", privateClean, privateReview)
	}
	publicReviewCandidates := len(scan.Rules[1].Candidates)
	detected := scan.Rules[1].Detected

	scan.ReleaseScanOnlyManifests()

	if len(privateClean.candidates) != 1 || len(privateClean.roots) != 1 {
		t.Fatal("cleanup-capable manifest was released")
	}
	if privateReview.candidates != nil || privateReview.roots != nil {
		t.Fatal("scan-only recursive manifest remains retained")
	}
	if len(scan.Rules[1].Candidates) != publicReviewCandidates || scan.Rules[1].Detected != detected {
		t.Fatal("public scan-only results changed during manifest release")
	}
	if _, err := engine.Clean(context.Background(), scan, CleanOptions{RuleIDs: []string{reviewRule.ID}}); !errors.Is(err, ErrScanOnly) {
		t.Fatalf("Clean error = %v, want ErrScanOnly", err)
	}
}

func TestRuleCompletedEventCarriesDataAndPathsArePrivate(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "cache")
	mustWrite(t, filepath.Join(root, "app", "data"), "data")
	engine := mustEngine(t, home, testRule("cache", root))
	var completed *RuleScan
	scan, err := engine.Scan(context.Background(), ScanOptions{Callback: func(event Event) {
		if event.Kind == EventRuleCompleted {
			completed = event.Rule
		}
	}})
	if err != nil {
		t.Fatal(err)
	}
	if completed == nil || len(completed.Candidates) != 1 {
		t.Fatalf("completed rule payload = %#v", completed)
	}
	path := scan.Rules[0].Candidates[0].DisplayPath
	if !strings.HasPrefix(path, "~/") || strings.Contains(path, home) {
		t.Fatalf("display path leaked home: %q", path)
	}
	encoded, err := json.Marshal(scan)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), home) {
		t.Fatalf("scan JSON leaked full home path: %s", encoded)
	}
}

func TestDefaultRulesAreNarrowAndDeveloperCachesAreAgeGated(t *testing.T) {
	home := t.TempDir()
	rules := DefaultRules(home)
	if len(rules) != 10 {
		t.Fatalf("got %d default rules, want 10", len(rules))
	}
	for _, rule := range rules {
		for _, root := range rule.Roots {
			if !strictDescendant(home, root) {
				t.Fatalf("rule %s has out-of-home root %s", rule.ID, root)
			}
		}
		if rule.ID == RuleDeveloperCaches {
			if rule.MinimumAge != 30*24*time.Hour || rule.Auto {
				t.Fatalf("developer cache rule = %#v", rule)
			}
		}
		if rule.ID == RuleXcodeArchives || rule.ID == RuleDeviceBackups {
			if rule.Action != ActionScanOnly {
				t.Fatalf("archive/backup rule %s is not scan-only", rule.ID)
			}
		}
	}
}

func TestVolumeStatsAndFormatBytes(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("volume stats unsupported on this platform")
	}
	volume, err := VolumeStats(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if volume.TotalBytes <= 0 || volume.FreeBytes < 0 || volume.AvailableBytes < 0 {
		t.Fatalf("volume = %#v", volume)
	}
	if got := FormatBytes(1024 * 1024 * 3); got != "3.0 MiB" {
		t.Fatalf("FormatBytes = %q", got)
	}
}

func testRule(id, root string) Rule {
	return Rule{ID: id, Name: id, Risk: RiskSafe, Action: ActionClean, Roots: []string{root}}
}

func mustEngine(t *testing.T, home string, rules ...Rule) *Engine {
	t.Helper()
	engine, err := New(home, rules)
	if err != nil {
		t.Fatal(err)
	}
	return engine
}

func mustWrite(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

func setTimes(t *testing.T, path string, when time.Time) {
	t.Helper()
	if err := os.Chtimes(path, when, when); err != nil {
		t.Fatal(err)
	}
}

func candidatesByBase(candidates []Candidate) map[string]Candidate {
	result := make(map[string]Candidate, len(candidates))
	for _, candidate := range candidates {
		result[filepath.Base(candidate.DisplayPath)] = candidate
	}
	return result
}
