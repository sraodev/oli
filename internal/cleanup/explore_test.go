package cleanup

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestExploreIsReadOnlyCountsHardlinksOnceAndRanksFiles(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "Downloads")
	large := filepath.Join(root, "folder", "large.bin")
	mustWrite(t, large, strings.Repeat("a", 200))
	mustWrite(t, filepath.Join(root, "small.bin"), "small")
	old := time.Now().AddDate(0, 0, -365)
	if err := os.Chtimes(large, old, old); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(large, filepath.Join(root, "folder", "alias.bin")); err != nil {
		t.Fatal(err)
	}
	external := t.TempDir()
	mustWrite(t, filepath.Join(external, "outside"), "never follow")
	if err := os.Symlink(external, filepath.Join(root, "external")); err != nil {
		t.Fatal(err)
	}
	engine, err := NewDefault(home)
	if err != nil {
		t.Fatal(err)
	}
	report, err := engine.Explore(context.Background(), ExploreOptions{MinSizeBytes: 100, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if report.Action != ActionScanOnly || report.Partial || report.Total.UniqueFiles != 2 || report.Total.LogicalBytes != 205 || report.Total.Symlinks != 1 {
		t.Fatalf("unexpected report: %+v", report)
	}
	if len(report.LargeFiles) != 1 || len(report.OldFiles) != 1 || len(report.Folders) != 1 || report.Folders[0].Metrics.LogicalBytes != 200 {
		t.Fatalf("unexpected ranked lists: %+v", report)
	}
	data, err := os.ReadFile(large)
	if err != nil || len(data) != 200 {
		t.Fatalf("inspection mutated file: %v", err)
	}
	raw, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), home) || strings.Contains(string(raw), "scan_id") || strings.Contains(string(raw), "reclaimable") {
		t.Fatalf("report leaked paths or deletion authority: %s", raw)
	}
}

func TestExploreRejectsSymlinkScopeAndDirectoryIdentitySwap(t *testing.T) {
	home := t.TempDir()
	out := t.TempDir()
	mustWrite(t, filepath.Join(out, "large"), "outside")
	if err := os.Symlink(out, filepath.Join(home, "Downloads")); err != nil {
		t.Fatal(err)
	}
	engine, err := NewDefault(home)
	if err != nil {
		t.Fatal(err)
	}
	report, err := engine.Explore(context.Background(), ExploreOptions{})
	if err != nil || !report.Partial || report.Total.Files != 0 {
		t.Fatalf("report=%+v err=%v", report, err)
	}
	parent, err := os.OpenRoot(home)
	if err != nil {
		t.Fatal(err)
	}
	defer parent.Close()
	mustWrite(t, filepath.Join(home, "real", "data"), "keep")
	_, expected, err := inspectInRoot(parent, "real")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(home, "real"), filepath.Join(home, "moved")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(home, "real"), 0o700); err != nil {
		t.Fatal(err)
	}
	walker := explorer{ctx: context.Background(), opts: ExploreOptions{Limit: 1, entryLimit: 100}, report: &Exploration{}, device: expected.Device, seen: map[string]bool{}}
	metrics, err := walker.directory(parent, "real", "~/real", expected, 0)
	if err != nil || metrics.Files != 0 || !walker.report.Partial || walker.report.Warnings[0].Code != "directory_changed" {
		t.Fatalf("identity swap accepted: %+v %v", walker.report, err)
	}
}

func TestExploreLimitsCancellationAndScopeValidation(t *testing.T) {
	home := t.TempDir()
	for _, name := range []string{"a", "b", "c"} {
		mustWrite(t, filepath.Join(home, "Downloads", name), "data")
	}
	engine, err := NewDefault(home)
	if err != nil {
		t.Fatal(err)
	}
	report, err := engine.Explore(context.Background(), ExploreOptions{entryLimit: 1})
	if err != nil || !report.Partial || report.Entries != 1 {
		t.Fatalf("report=%+v err=%v", report, err)
	}
	for _, opts := range []ExploreOptions{{Scope: "../../"}, {Limit: 201}, {MinSizeBytes: -1}, {OlderThanDays: -1}} {
		if _, err := engine.Explore(context.Background(), opts); err == nil {
			t.Fatalf("accepted %+v", opts)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := engine.Explore(ctx, ExploreOptions{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("error=%v", err)
	}
}

func TestExploreTopListsBoundedWithStableTies(t *testing.T) {
	items := []ExploreItem{}
	for _, name := range []string{"z", "b", "a"} {
		items = topExploreItems(items, ExploreItem{DisplayPath: name, Metrics: Metrics{LogicalBytes: 10}}, 2)
	}
	if len(items) != 2 || items[0].DisplayPath != "a" || items[1].DisplayPath != "b" {
		t.Fatalf("items=%+v", items)
	}
}

func TestExploreReportsUnreadableDirectoryWithoutBypassingPermissions(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses normal directory permissions")
	}
	home := t.TempDir()
	locked := filepath.Join(home, "Downloads", "locked")
	mustWrite(t, filepath.Join(locked, "private"), "private")
	if err := os.Chmod(locked, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o700) })
	engine, err := NewDefault(home)
	if err != nil {
		t.Fatal(err)
	}
	report, err := engine.Explore(context.Background(), ExploreOptions{})
	if err != nil || !report.Partial || report.Total.Files != 0 || len(report.Warnings) == 0 {
		t.Fatalf("report=%+v err=%v", report, err)
	}
	if report.Warnings[0].Code == "directory_changed" {
		t.Fatal("permission failure incorrectly reported as a race")
	}
}
