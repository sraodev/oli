package app

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/sraodev/mac-cleanup-studio/internal/cleanup"
	"github.com/sraodev/mac-cleanup-studio/internal/dashboard"
)

// DashboardService adapts the filesystem engine to the path-free dashboard
// protocol. Only the most recent completed scan stays in memory, and its plan
// is single-use for cleanup.
type DashboardService struct {
	engine *cleanup.Engine
	home   string

	operation chan struct{}
	mu        sync.Mutex
	current   *cleanup.Scan
}

func NewDashboardService(engine *cleanup.Engine, home string) (*DashboardService, error) {
	if engine == nil {
		return nil, errors.New("app: cleanup engine is required")
	}
	if home == "" {
		return nil, errors.New("app: home path is required")
	}
	return &DashboardService{
		engine: engine, home: home,
		operation: make(chan struct{}, 1),
	}, nil
}

func (s *DashboardService) Disk(context.Context) (dashboard.DiskUsage, error) {
	volume, err := cleanup.VolumeStats(s.home)
	if err != nil {
		return dashboard.DiskUsage{}, err
	}
	return dashboard.DiskUsage{
		Volume:     "Home volume",
		TotalBytes: nonnegative(volume.TotalBytes),
		UsedBytes:  nonnegative(volume.UsedBytes),
		FreeBytes:  nonnegative(volume.AvailableBytes),
	}, nil
}

func (s *DashboardService) Scan(ctx context.Context, request dashboard.ScanRequest, send func(dashboard.ScanEvent) error) error {
	release, err := s.beginOperation()
	if err != nil {
		return err
	}
	defer release()

	ruleIDs, err := RuleIDs(s.engine.Rules(), request.Profile, false)
	if err != nil {
		return err
	}
	// Starting a new scan invalidates and releases the previous frozen plan.
	// This also prevents two recursive manifests from being retained while the
	// new scan is running.
	s.clearCurrent()
	if err := send(dashboard.ScanEvent{
		Type: "scan.started", Message: "Scanning compiled user-level locations",
	}); err != nil {
		return err
	}

	started := time.Now()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var sendErr error
	var completed int
	var filesScanned, bytesScanned, reclaimable uint64
	callback := func(event cleanup.Event) {
		if sendErr != nil {
			return
		}
		switch event.Kind {
		case cleanup.EventRuleStarted:
			sendErr = send(dashboard.ScanEvent{
				Type: "scan.progress", Message: "Scanning " + ruleName(s.engine.Rules(), event.RuleID),
				Progress: progress(completed, len(ruleIDs)), FilesScanned: filesScanned,
				BytesScanned: bytesScanned, ReclaimableBytes: reclaimable,
			})
		case cleanup.EventWarning:
			message := event.Message
			ruleID := event.RuleID
			if event.Warning != nil {
				message = event.Warning.Message
				ruleID = event.Warning.RuleID
			}
			sendErr = send(dashboard.ScanEvent{Type: "scan.issue", Issue: &dashboard.ScanIssue{
				RuleID: ruleID, Message: message, Recoverable: true,
			}})
		case cleanup.EventRuleCompleted:
			if event.Rule == nil {
				return
			}
			completed++
			filesScanned += nonnegative(event.Rule.Detected.Files)
			bytesScanned += nonnegative(event.Rule.Detected.AllocatedBytes)
			reclaimable += nonnegative(event.Rule.Eligible.AllocatedBytes)
			category := categoryView(*event.Rule)
			sendErr = send(dashboard.ScanEvent{
				Type: "scan.category", Category: &category,
				Progress: progress(completed, len(ruleIDs)), FilesScanned: filesScanned,
				BytesScanned: bytesScanned, ReclaimableBytes: reclaimable,
			})
		}
		if sendErr != nil {
			cancel()
		}
	}

	scan, scanErr := s.engine.Scan(ctx, cleanup.ScanOptions{RuleIDs: ruleIDs, Callback: callback})
	if sendErr != nil {
		return sendErr
	}
	if scanErr != nil {
		return scanErr
	}
	scan.ReleaseScanOnlyManifests()
	s.remember(scan)
	if err := send(dashboard.ScanEvent{
		Type: "scan.complete", ScanID: scan.ID,
		FilesScanned: filesScanned, BytesScanned: bytesScanned, ReclaimableBytes: reclaimable,
		Progress: 1,
		Summary: &dashboard.ScanSummary{
			ScanID: scan.ID, FilesScanned: filesScanned, BytesScanned: bytesScanned,
			ReclaimableBytes: reclaimable, DurationMillis: time.Since(started).Milliseconds(),
		},
	}); err != nil {
		s.discard(scan.ID)
		return err
	}
	return nil
}

func (s *DashboardService) Clean(ctx context.Context, request dashboard.CleanRequest, send func(dashboard.CleanEvent) error) error {
	if request.Confirmation != "DELETE" {
		return errors.New("app: cleanup confirmation is invalid")
	}
	release, err := s.beginOperation()
	if err != nil {
		return err
	}
	defer release()

	scan, ok := s.lookup(request.ScanID)
	if !ok {
		return errors.New("app: scan is unknown, expired, or already used")
	}
	selected, err := ValidateRuleIDs(s.engine.Rules(), request.RuleIDs)
	if err != nil {
		return err
	}
	selectedBytes, candidates := selectedPlan(scan, selected)
	if len(candidates) == 0 {
		return errors.New("app: selected rules have no eligible candidates")
	}

	before, err := cleanup.VolumeStats(s.home)
	if err != nil {
		return fmt.Errorf("app: read disk usage before cleanup: %w", err)
	}
	if err := send(dashboard.CleanEvent{
		Type: "clean.started", Message: "Revalidating the immutable scan plan",
	}); err != nil {
		return err
	}
	consumed, ok := s.take(request.ScanID)
	if !ok || consumed != scan {
		return errors.New("app: scan expired before cleanup could start")
	}

	started := time.Now()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var sendErr error
	var finished, removedItems, removedBytes uint64
	callback := func(event cleanup.Event) {
		if sendErr != nil {
			return
		}
		candidate, known := candidates[event.CandidateID]
		switch event.Kind {
		case cleanup.EventCandidateRemoved:
			finished++
			removedItems++
			if known {
				removedBytes += nonnegative(candidate.Metrics.AllocatedBytes)
			}
			sendErr = send(dashboard.CleanEvent{
				Type: "clean.progress", RuleID: event.RuleID,
				Message: "Removed " + event.DisplayPath, Progress: progress64(finished, uint64(len(candidates))),
				RemovedItems: removedItems, RemovedBytes: removedBytes,
			})
		case cleanup.EventCandidateRejected, cleanup.EventCandidateSkipped:
			// The engine reports age-ineligible candidates as skipped for audit
			// purposes. They were never part of the selected cleanup total.
			if !known {
				return
			}
			finished++
			message := event.Message
			if message == "" {
				message = "Skipped " + event.DisplayPath + " because it changed after scanning"
			}
			sendErr = send(dashboard.CleanEvent{
				Type: "clean.issue", RuleID: event.RuleID,
				Progress:     progress64(finished, uint64(len(candidates))),
				RemovedItems: removedItems, RemovedBytes: removedBytes,
				Issue: &dashboard.CleanIssue{RuleID: event.RuleID, Message: message},
			})
		case cleanup.EventCandidateFailed:
			if !known {
				return
			}
			finished++
			message := event.Message
			if event.Failure != nil {
				message = event.Failure.Message
			}
			if message == "" {
				message = "Could not remove " + event.DisplayPath
			}
			sendErr = send(dashboard.CleanEvent{
				Type: "clean.issue", RuleID: event.RuleID,
				Progress:     progress64(finished, uint64(len(candidates))),
				RemovedItems: removedItems, RemovedBytes: removedBytes,
				Issue: &dashboard.CleanIssue{RuleID: event.RuleID, Message: message},
			})
		}
		if sendErr != nil {
			cancel()
		}
	}

	result, cleanErr := s.engine.Clean(ctx, scan, cleanup.CleanOptions{RuleIDs: selected, Callback: callback})
	if sendErr != nil {
		return sendErr
	}
	if cleanErr != nil {
		if result != nil {
			after, afterErr := cleanup.VolumeStats(s.home)
			if afterErr != nil {
				after = before
			}
			if err := send(dashboard.CleanEvent{
				Type: "clean.partial", Message: "Cleanup stopped after making partial progress.",
				Progress:     progress64(uint64(len(result.Removed)+len(result.Rejected)+len(result.Failures)), uint64(len(candidates))),
				RemovedItems: uint64(len(result.Removed)), RemovedBytes: nonnegative(result.Freed.AllocatedBytes),
				Summary: cleanSummary(result, selectedBytes, before, after, time.Since(started), true),
			}); err != nil {
				return err
			}
		}
		return cleanErr
	}
	after, err := cleanup.VolumeStats(s.home)
	if err != nil {
		return fmt.Errorf("app: read disk usage after cleanup: %w", err)
	}
	removedItems = uint64(len(result.Removed))
	removedBytes = nonnegative(result.Freed.AllocatedBytes)
	return send(dashboard.CleanEvent{
		Type: "clean.complete", Progress: 1, RemovedItems: removedItems, RemovedBytes: removedBytes,
		Summary: cleanSummary(result, selectedBytes, before, after, time.Since(started), false),
	})
}

func (s *DashboardService) beginOperation() (func(), error) {
	select {
	case s.operation <- struct{}{}:
		return func() { <-s.operation }, nil
	default:
		return nil, errors.New("app: another scan or cleanup is already running")
	}
}

func (s *DashboardService) remember(scan *cleanup.Scan) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.current = scan
}

func (s *DashboardService) take(id string) (*cleanup.Scan, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.current == nil || s.current.ID != id {
		return nil, false
	}
	scan := s.current
	s.current = nil
	return scan, true
}

func (s *DashboardService) lookup(id string) (*cleanup.Scan, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.current == nil || s.current.ID != id {
		return nil, false
	}
	return s.current, true
}

func (s *DashboardService) clearCurrent() {
	s.mu.Lock()
	s.current = nil
	s.mu.Unlock()
}

func (s *DashboardService) discard(id string) {
	s.mu.Lock()
	if s.current != nil && s.current.ID == id {
		s.current = nil
	}
	s.mu.Unlock()
}

func cleanSummary(result *cleanup.CleanResult, selectedBytes uint64, before, after cleanup.VolumeInfo, duration time.Duration, partial bool) *dashboard.CleanSummary {
	return &dashboard.CleanSummary{
		SelectedBytes: selectedBytes, RemovedBytes: nonnegative(result.Freed.AllocatedBytes),
		MeasuredReclaimedBytes: positiveDifference(after.AvailableBytes, before.AvailableBytes),
		RemovedItems:           uint64(len(result.Removed)),
		FailedItems:            uint64(len(result.Failures) + len(result.Rejected)),
		BeforeFreeBytes:        nonnegative(before.AvailableBytes),
		AfterFreeBytes:         nonnegative(after.AvailableBytes),
		DurationMillis:         duration.Milliseconds(),
		Partial:                partial,
	}
}

func categoryView(rule cleanup.RuleScan) dashboard.ScanCategory {
	candidates := append([]cleanup.Candidate(nil), rule.Candidates...)
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].Metrics.AllocatedBytes > candidates[j].Metrics.AllocatedBytes
	})
	items := make([]dashboard.ScanItem, 0, len(candidates))
	for _, candidate := range candidates {
		if rule.Rule.Action == cleanup.ActionClean && !candidate.Eligible {
			continue
		}
		items = append(items, dashboard.ScanItem{
			Name: candidate.DisplayPath, Location: candidate.RootDisplayPath,
			SizeBytes:   nonnegative(candidate.Metrics.AllocatedBytes),
			ModifiedAge: modifiedAge(candidate.LatestModified),
		})
	}
	return dashboard.ScanCategory{
		RuleID: rule.Rule.ID, Name: rule.Rule.Name, Description: rule.Rule.Description,
		Risk: riskView(rule.Rule.Risk), Age: ageView(rule.Rule.MinimumAgeSeconds),
		Action: dashboard.RuleAction(rule.Rule.Action), ItemCount: uint64(len(items)),
		DetectedBytes:     nonnegative(rule.Detected.AllocatedBytes),
		ReclaimableBytes:  nonnegative(rule.Eligible.AllocatedBytes),
		SelectedByDefault: rule.Rule.Default && rule.Rule.Action == cleanup.ActionClean,
		LargestItems:      items,
	}
}

func selectedPlan(scan *cleanup.Scan, ruleIDs []string) (uint64, map[string]cleanup.Candidate) {
	wanted := make(map[string]struct{}, len(ruleIDs))
	for _, id := range ruleIDs {
		wanted[id] = struct{}{}
	}
	items := make(map[string]cleanup.Candidate)
	var bytes uint64
	for _, rule := range scan.Rules {
		if _, ok := wanted[rule.Rule.ID]; !ok {
			continue
		}
		bytes += nonnegative(rule.Eligible.AllocatedBytes)
		for _, candidate := range rule.Candidates {
			if !candidate.Eligible {
				continue
			}
			items[candidate.ID] = candidate
		}
	}
	return bytes, items
}

func ruleName(rules []cleanup.RuleInfo, id string) string {
	for _, rule := range rules {
		if rule.ID == id {
			return rule.Name
		}
	}
	return id
}

func riskView(risk cleanup.Risk) dashboard.Risk {
	switch risk {
	case cleanup.RiskSafe:
		return dashboard.RiskLow
	case cleanup.RiskCaution:
		return dashboard.RiskMedium
	default:
		return dashboard.RiskHigh
	}
}

func ageView(seconds int64) string {
	if seconds <= 0 {
		return "No age gate"
	}
	duration := time.Duration(seconds) * time.Second
	if duration%(24*time.Hour) == 0 {
		return fmt.Sprintf("At least %d days old", duration/(24*time.Hour))
	}
	return "At least " + duration.String() + " old"
}

func modifiedAge(modified time.Time) string {
	if modified.IsZero() {
		return "Unknown age"
	}
	days := int(time.Since(modified).Hours() / 24)
	if days < 1 {
		return "Modified today"
	}
	if days == 1 {
		return "Modified 1 day ago"
	}
	return fmt.Sprintf("Modified %d days ago", days)
}

func progress(done, total int) float64 {
	return progress64(uint64(done), uint64(total))
}

func progress64(done, total uint64) float64 {
	if total == 0 {
		return 1
	}
	value := float64(done) / float64(total)
	if value > 1 {
		return 1
	}
	return value
}

func nonnegative(value int64) uint64 {
	if value <= 0 {
		return 0
	}
	return uint64(value)
}

func positiveDifference(after, before int64) uint64 {
	if after <= before {
		return 0
	}
	return uint64(after - before)
}
