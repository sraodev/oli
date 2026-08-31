package cleanup

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestExploreFixedScopesAndThresholdBoundaries(t *testing.T) {
	home := t.TempDir()
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	cutoff := now.AddDate(0, 0, -180)
	for _, file := range []struct {
		name     string
		size     int
		modified time.Time
	}{
		{"at-boundary", 100, cutoff},
		{"too-small", 99, cutoff},
		{"too-recent", 101, cutoff.Add(time.Second)},
	} {
		path := filepath.Join(home, "Downloads", file.name)
		mustWrite(t, path, strings.Repeat("x", file.size))
		if err := os.Chtimes(path, file.modified, file.modified); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite(t, filepath.Join(home, "Documents", "keep"), "keep")
	mustWrite(t, filepath.Join(home, "outside-scopes", "keep"), "not in report")
	engine, err := NewDefault(home)
	if err != nil {
		t.Fatal(err)
	}
	report, err := engine.Explore(context.Background(), ExploreOptions{Now: now, MinSizeBytes: 100})
	if err != nil {
		t.Fatal(err)
	}
	if report.Partial || report.Total.LogicalBytes != 300 || len(report.Scopes) != 1 || report.Scopes[0].ID != "downloads" {
		t.Fatalf("scope isolation failed: %+v", report)
	}
	wantLarge := []string{"~/Downloads/too-recent", "~/Downloads/at-boundary"}
	wantOld := []string{"~/Downloads/at-boundary", "~/Downloads/too-small"}
	for _, list := range []struct {
		items []ExploreItem
		want  []string
	}{{report.LargeFiles, wantLarge}, {report.OldFiles, wantOld}} {
		var got []string
		for _, item := range list.items {
			got = append(got, item.DisplayPath)
		}
		if !reflect.DeepEqual(got, list.want) {
			t.Fatalf("paths=%v want=%v", got, list.want)
		}
	}
	all, err := engine.Explore(context.Background(), ExploreOptions{Scope: "all"})
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, scope := range all.Scopes {
		ids = append(ids, scope.ID)
	}
	wantIDs := []string{"downloads", "documents", "desktop", "movies", "music", "pictures", "applications"}
	if all.Partial || all.Total.LogicalBytes != 304 || !reflect.DeepEqual(ids, wantIDs) {
		t.Fatalf("all scopes=%v total=%+v partial=%v", ids, all.Total, all.Partial)
	}
	if all.MinSizeBytes != 100<<20 || all.OlderDays != 180 || all.Limit != 50 {
		t.Fatalf("defaults=%+v", all)
	}
}

func TestExploreDepthDeviceAndWarningBounds(t *testing.T) {
	home := t.TempDir()
	mustWrite(t, filepath.Join(home, "Downloads", "keep"), "keep")
	root, err := os.OpenRoot(home)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	_, fp, err := inspectInRoot(root, "Downloads")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		depth  int
		device uint64
	}{
		{"depth", 65, fp.Device}, {"device", 0, fp.Device + 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			walker := explorer{ctx: context.Background(), report: &Exploration{}, device: test.device}
			metrics, err := walker.directory(root, "Downloads", "~/Downloads", fp, test.depth)
			if err != nil || metrics.Files != 0 || !walker.report.Partial || len(walker.report.Warnings) != 1 {
				t.Fatalf("metrics=%+v report=%+v err=%v", metrics, walker.report, err)
			}
		})
	}
	walker := explorer{report: &Exploration{}}
	for i := 0; i < 101; i++ {
		walker.warn("~/Downloads", "test", "test warning")
	}
	if !walker.report.Partial || len(walker.report.Warnings) != 100 {
		t.Fatalf("warnings=%+v", walker.report)
	}
}

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
