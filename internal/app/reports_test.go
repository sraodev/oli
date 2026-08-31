package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sraodev/mac-cleanup-studio/internal/cleanup"
	"github.com/sraodev/mac-cleanup-studio/internal/dashboard"
)

func TestScanSummaryDeduplicatesAcrossRules(t *testing.T) {
	home := t.TempDir()
	writeFixture(t, filepath.Join(home, "a", "file"), "one inode")
	if err := os.Mkdir(filepath.Join(home, "b"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(filepath.Join(home, "a", "file"), filepath.Join(home, "b", "alias")); err != nil {
		t.Fatal(err)
	}
	rules := []cleanup.Rule{}
	for _, id := range []string{"a", "b"} {
		rules = append(rules, cleanup.Rule{ID: id, Name: id, Action: cleanup.ActionClean, Auto: true, MinimumAge: 1, Risk: cleanup.RiskSafe, Roots: []string{filepath.Join(home, id)}})
	}
	engine, err := cleanup.New(home, rules)
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewDashboardService(engine, home)
	if err != nil {
		t.Fatal(err)
	}
	var summary *dashboard.ScanSummary
	var sum uint64
	err = service.Scan(context.Background(), dashboard.ScanRequest{Profile: dashboard.ProfileSafe}, func(event dashboard.ScanEvent) error {
		if event.Category != nil {
			sum += event.Category.DetectedBytes
		}
		if event.Summary != nil {
			summary = event.Summary
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if summary == nil || summary.LogicalBytes != uint64(len("one inode")) || sum != 2*summary.BytesScanned || summary.Partial {
		t.Fatalf("summary=%+v sum=%d", summary, sum)
	}
}

func TestCleanSummaryPreservesNegativeObservationAndPartialOutcome(t *testing.T) {
	result := &cleanup.CleanResult{Rejected: []cleanup.CandidateOutcome{{CandidateID: "changed"}}}
	summary := cleanSummary(result, 123, cleanup.VolumeInfo{AvailableBytes: 1000}, cleanup.VolumeInfo{AvailableBytes: 800}, time.Second, false)
	if summary.ObservedFreeSpaceChangeBytes != -200 || summary.MeasuredReclaimedBytes != 0 || !summary.ObservationAvailable || !summary.Partial {
		t.Fatalf("summary=%+v", summary)
	}
}

func TestExcludedCandidateIsNotRecommended(t *testing.T) {
	home := t.TempDir()
	writeFixture(t, filepath.Join(home, "cache", "keep"), "keep")
	writeFixture(t, filepath.Join(home, cleanup.ProtectionConfig), `{"version":1,"rules":["cache"]}`)
	engine, err := cleanup.New(home, []cleanup.Rule{{ID: "cache", Name: "cache", Action: cleanup.ActionClean, Auto: true, MinimumAge: 1, Risk: cleanup.RiskSafe, Roots: []string{filepath.Join(home, "cache")}}})
	if err != nil {
		t.Fatal(err)
	}
	scan, err := engine.Scan(context.Background(), cleanup.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range Recommendations(scan) {
		if r.Decision == DecisionAutoClean || r.Reclaimable.LogicalBytes != 0 {
			t.Fatalf("recommendation=%+v", r)
		}
	}
}
