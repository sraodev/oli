package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sraodev/mac-cleanup-studio/internal/cleanup"
	"github.com/sraodev/mac-cleanup-studio/internal/dashboard"
)

func TestDashboardServiceExploreIsSerializedAndCreatesNoCleanupPlan(t *testing.T) {
	home := t.TempDir()
	writeFixture(t, filepath.Join(home, "Downloads", "keep"), "keep")
	engine, err := cleanup.NewDefault(home)
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewDashboardService(engine, home)
	if err != nil {
		t.Fatal(err)
	}
	release, err := service.beginOperation()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Explore(context.Background(), "downloads"); err == nil {
		t.Fatal("explore accepted an overlapping operation")
	}
	release()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := service.Explore(ctx, "downloads"); !errors.Is(err, context.Canceled) {
		t.Fatalf("error=%v", err)
	}
	report, err := service.Explore(context.Background(), "downloads")
	if err != nil {
		t.Fatal(err)
	}
	if report.Action != cleanup.ActionScanOnly || report.Total.UniqueFiles != 1 || service.current != nil {
		t.Fatalf("report=%+v current=%+v", report, service.current)
	}
	if data, err := os.ReadFile(filepath.Join(home, "Downloads", "keep")); err != nil || string(data) != "keep" {
		t.Fatalf("fixture changed: %q %v", data, err)
	}
}

func TestDashboardServiceScanAndSingleUseClean(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "cache")
	oldCandidate := filepath.Join(root, "old-app")
	recentCandidate := filepath.Join(root, "recent-app")
	writeFixture(t, filepath.Join(oldCandidate, "data"), "old cache")
	writeFixture(t, filepath.Join(recentCandidate, "data"), "recent cache")
	old := time.Now().Add(-48 * time.Hour)
	setFixtureTimes(t, filepath.Join(oldCandidate, "data"), old)
	setFixtureTimes(t, oldCandidate, old)

	rule := cleanup.Rule{
		ID: "old-cache", Name: "Old cache", Risk: cleanup.RiskSafe,
		Action: cleanup.ActionClean, Default: true, Auto: true,
		MinimumAge: 24 * time.Hour, Roots: []string{root},
	}
	engine, err := cleanup.New(home, []cleanup.Rule{rule})
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewDashboardService(engine, home)
	if err != nil {
		t.Fatal(err)
	}

	var scanID string
	var category *dashboard.ScanCategory
	err = service.Scan(context.Background(), dashboard.ScanRequest{Profile: dashboard.ProfileSafe}, func(event dashboard.ScanEvent) error {
		if event.Category != nil {
			value := *event.Category
			category = &value
		}
		if event.Summary != nil {
			scanID = event.Summary.ScanID
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if scanID == "" || category == nil {
		t.Fatalf("scanID=%q category=%#v", scanID, category)
	}
	if category.Action != dashboard.ActionClean || category.ItemCount != 1 || category.ReclaimableBytes == 0 {
		t.Fatalf("category = %#v", category)
	}

	var issues int
	var summary *dashboard.CleanSummary
	err = service.Clean(context.Background(), dashboard.CleanRequest{
		ScanID: scanID, RuleIDs: []string{rule.ID}, Confirmation: "DELETE",
	}, func(event dashboard.CleanEvent) error {
		if event.Type == "clean.issue" {
			issues++
		}
		if event.Summary != nil {
			value := *event.Summary
			summary = &value
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if issues != 0 {
		t.Fatalf("age-ineligible candidate produced %d cleanup issues", issues)
	}
	if summary == nil || summary.RemovedItems != 1 || summary.FailedItems != 0 {
		t.Fatalf("summary = %#v", summary)
	}
	if _, err := os.Stat(oldCandidate); !os.IsNotExist(err) {
		t.Fatalf("old candidate remains: %v", err)
	}
	if _, err := os.Stat(recentCandidate); err != nil {
		t.Fatalf("recent candidate was removed: %v", err)
	}

	err = service.Clean(context.Background(), dashboard.CleanRequest{
		ScanID: scanID, RuleIDs: []string{rule.ID}, Confirmation: "DELETE",
	}, func(dashboard.CleanEvent) error { return nil })
	if err == nil {
		t.Fatal("completed scan plan was reusable")
	}
}

func TestDashboardServiceRejectsBadConfirmationWithoutConsumingScan(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "cache")
	writeFixture(t, filepath.Join(root, "app", "data"), "cache")
	rule := cleanup.Rule{
		ID: "cache", Name: "Cache", Risk: cleanup.RiskSafe, Action: cleanup.ActionClean,
		Default: true, Auto: true, MinimumAge: time.Nanosecond, Roots: []string{root},
	}
	engine, err := cleanup.New(home, []cleanup.Rule{rule})
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewDashboardService(engine, home)
	if err != nil {
		t.Fatal(err)
	}
	var scanID string
	err = service.Scan(context.Background(), dashboard.ScanRequest{Profile: dashboard.ProfileSafe}, func(event dashboard.ScanEvent) error {
		if event.Summary != nil {
			scanID = event.Summary.ScanID
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Clean(context.Background(), dashboard.CleanRequest{
		ScanID: scanID, RuleIDs: []string{rule.ID}, Confirmation: "delete",
	}, func(dashboard.CleanEvent) error { return nil }); err == nil {
		t.Fatal("bad confirmation was accepted")
	}
	if _, ok := service.lookup(scanID); !ok {
		t.Fatal("bad confirmation consumed scan plan")
	}
}

func TestDashboardServiceRetainsOnlyLatestScan(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "cache")
	writeFixture(t, filepath.Join(root, "app", "data"), "cache")
	rule := cleanup.Rule{
		ID: "cache", Name: "Cache", Risk: cleanup.RiskSafe, Action: cleanup.ActionClean,
		Default: true, Auto: true, MinimumAge: time.Nanosecond, Roots: []string{root},
	}
	engine, err := cleanup.New(home, []cleanup.Rule{rule})
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewDashboardService(engine, home)
	if err != nil {
		t.Fatal(err)
	}

	scan := func() string {
		t.Helper()
		var id string
		if err := service.Scan(context.Background(), dashboard.ScanRequest{Profile: dashboard.ProfileSafe}, func(event dashboard.ScanEvent) error {
			if event.Summary != nil {
				id = event.Summary.ScanID
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		return id
	}
	first := scan()
	second := scan()
	if first == "" || second == "" || first == second {
		t.Fatalf("scan IDs first=%q second=%q", first, second)
	}
	if _, ok := service.lookup(first); ok {
		t.Fatal("superseded scan plan remains retained")
	}
	if current, ok := service.lookup(second); !ok || current.ID != second {
		t.Fatal("latest scan plan is not retained")
	}
}

func TestDashboardServiceReportsPartialResultOnCancellation(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "cache")
	writeFixture(t, filepath.Join(root, "first", "data"), "first cache")
	writeFixture(t, filepath.Join(root, "second", "data"), "second cache")
	rule := cleanup.Rule{
		ID: "cache", Name: "Cache", Risk: cleanup.RiskSafe, Action: cleanup.ActionClean,
		Default: true, Auto: true, MinimumAge: time.Nanosecond, Roots: []string{root},
	}
	engine, err := cleanup.New(home, []cleanup.Rule{rule})
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewDashboardService(engine, home)
	if err != nil {
		t.Fatal(err)
	}
	var scanID string
	if err := service.Scan(context.Background(), dashboard.ScanRequest{Profile: dashboard.ProfileSafe}, func(event dashboard.ScanEvent) error {
		if event.Summary != nil {
			scanID = event.Summary.ScanID
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var partial *dashboard.CleanSummary
	err = service.Clean(ctx, dashboard.CleanRequest{
		ScanID: scanID, RuleIDs: []string{rule.ID}, Confirmation: "DELETE",
	}, func(event dashboard.CleanEvent) error {
		if event.Type == "clean.progress" && event.RemovedItems == 1 {
			cancel()
		}
		if event.Type == "clean.partial" && event.Summary != nil {
			value := *event.Summary
			partial = &value
		}
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Clean error = %v, want context.Canceled", err)
	}
	if partial == nil || !partial.Partial || partial.RemovedItems != 1 || partial.RemovedBytes == 0 {
		t.Fatalf("partial summary = %#v", partial)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("remaining candidates = %d, want 1", len(entries))
	}
	if _, ok := service.lookup(scanID); ok {
		t.Fatal("partially used scan plan remains retained")
	}
}

func writeFixture(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

func setFixtureTimes(t *testing.T, path string, when time.Time) {
	t.Helper()
	if err := os.Chtimes(path, when, when); err != nil {
		t.Fatal(err)
	}
}
