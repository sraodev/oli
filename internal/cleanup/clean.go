package cleanup

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Clean removes selected eligible candidates from a single-use private scan.
// An empty selection never means "all".
func (e *Engine) Clean(ctx context.Context, scan *Scan, opts CleanOptions) (*CleanResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	if scan == nil || scan.plan == nil || scan.plan.engineToken != e.token {
		return nil, ErrInvalidScan
	}
	scan.plan.mu.Lock()
	selection, err := e.selectCandidates(scan.plan, opts)
	if err != nil {
		scan.plan.mu.Unlock()
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		scan.plan.mu.Unlock()
		return &CleanResult{ScanID: scan.plan.scanID, StartedAt: time.Now(), CompletedAt: time.Now(), RuleIDs: selection.RuleIDs}, err
	}
	scan.plan.consumed = true
	// Claim the plan before callbacks or filesystem work. A reentrant callback
	// may inspect the consumed state without deadlocking this cleanup.
	scan.plan.mu.Unlock()
	selectedIDs := selection.RuleIDs
	selectedCandidates := make(map[string]bool, len(selection.CandidateIDs))
	for _, id := range selection.CandidateIDs {
		selectedCandidates[id] = true
	}

	result := &CleanResult{
		ScanID: scan.plan.scanID, StartedAt: time.Now(),
		RuleIDs: append([]string(nil), selectedIDs...),
	}
	emit(opts.Callback, Event{Kind: EventCleanStarted})

	selectedSet := make(map[string]struct{}, len(selectedIDs))
	for _, id := range selectedIDs {
		selectedSet[id] = struct{}{}
	}
	type plannedCandidate struct {
		rule *frozenRule
		item *frozenCandidate
	}
	var eligible []plannedCandidate
	for _, id := range scan.plan.order {
		if _, ok := selectedSet[id]; !ok {
			continue
		}
		rule := scan.plan.rules[id]
		for _, candidate := range rule.candidates {
			if opts.CandidateIDs != nil && !selectedCandidates[candidate.summary.ID] {
				continue
			}
			if !candidate.summary.Eligible {
				outcome := outcomeFor(candidate, candidate.summary.EligibilityReason)
				result.Skipped = append(result.Skipped, outcome)
				emit(opts.Callback, Event{
					Kind: EventCandidateSkipped, RuleID: id, CandidateID: candidate.summary.ID,
					DisplayPath: candidate.summary.DisplayPath, Message: outcome.Reason,
				})
				continue
			}
			eligible = append(eligible, plannedCandidate{rule: rule, item: candidate})
		}
	}

	// Revalidate every selected root before inspecting or mutating entries.
	rootErrors := make(map[string]string)
	for _, planned := range eligible {
		root := planned.rule.roots[planned.item.rootPath]
		if _, seen := rootErrors[root.path]; seen {
			continue
		}
		_, current, err := e.inspectRoot(root.path)
		if err != nil {
			rootErrors[root.path] = fmt.Sprintf("compiled root changed after scan: %s", publicError(e.home, err))
			continue
		}
		if !root.fingerprint.equal(current) {
			rootErrors[root.path] = "compiled root changed after scan"
		}
	}

	accepted := make([]plannedCandidate, 0, len(eligible))
	for _, planned := range eligible {
		if reason := rootErrors[planned.item.rootPath]; reason != "" {
			rejectCandidate(result, opts.Callback, planned.item, reason)
			continue
		}
		if err := validateFrozenCandidate(planned.item, nil); err != nil {
			rejectCandidate(result, opts.Callback, planned.item, fmt.Sprintf("entry changed after scan: %s", publicError(e.home, err)))
			continue
		}
		accepted = append(accepted, planned)
	}

	// A second root check closes changes that happened during entry preflight.
	finalAccepted := accepted[:0]
	for _, planned := range accepted {
		root := planned.rule.roots[planned.item.rootPath]
		_, current, err := e.inspectRoot(root.path)
		if err != nil || !root.fingerprint.equal(current) {
			rejectCandidate(result, opts.Callback, planned.item, "compiled root changed during cleanup preflight")
			continue
		}
		finalAccepted = append(finalAccepted, planned)
	}
	accepted = finalAccepted

	removedLinksByInode := make(map[string]uint64)
	var removedCandidates []*frozenCandidate
	for _, planned := range accepted {
		if err := ctx.Err(); err != nil {
			result.CompletedAt = time.Now()
			result.Freed = metricsForCandidates(removedCandidates, false)
			return result, err
		}
		protection, err := e.loadProtection()
		if err != nil {
			result.CompletedAt = time.Now()
			result.Freed = metricsForCandidates(removedCandidates, false)
			return result, err
		}
		if protection.protects(planned.item) {
			rejectCandidate(result, opts.Callback, planned.item, "protected by current exclusions")
			continue
		}
		// Repeat the whole-candidate check immediately before its first
		// mutation. This catches an unplanned child created after global
		// preflight without partially deleting the candidate.
		if err := validateFrozenCandidate(planned.item, removedLinksByInode); err != nil {
			rejectCandidate(result, opts.Callback, planned.item, fmt.Sprintf("entry changed before cleanup: %s", publicError(e.home, err)))
			continue
		}
		root := planned.rule.roots[planned.item.rootPath]
		removed, changed, err := e.removeCandidate(ctx, root, planned.item, removedLinksByInode)
		if err != nil {
			if changed && !removed {
				rejectCandidate(result, opts.Callback, planned.item, fmt.Sprintf("entry changed during cleanup: %s", publicError(e.home, err)))
				continue
			}
			failure := CleanFailure{
				CandidateID: planned.item.summary.ID, RuleID: planned.item.summary.RuleID,
				DisplayPath: planned.item.summary.DisplayPath, Message: publicError(e.home, err),
			}
			result.Failures = append(result.Failures, failure)
			emit(opts.Callback, Event{
				Kind: EventCandidateFailed, RuleID: failure.RuleID, CandidateID: failure.CandidateID,
				DisplayPath: failure.DisplayPath, Message: failure.Message, Failure: &failure,
			})
			continue
		}
		removedCandidates = append(removedCandidates, planned.item)
		result.Removed = append(result.Removed, outcomeFor(planned.item, ""))
		emit(opts.Callback, Event{
			Kind: EventCandidateRemoved, RuleID: planned.item.summary.RuleID,
			CandidateID: planned.item.summary.ID, DisplayPath: planned.item.summary.DisplayPath,
		})
	}

	result.Freed = metricsForCandidates(removedCandidates, false)
	result.CompletedAt = time.Now()
	emit(opts.Callback, Event{Kind: EventCleanCompleted})
	return result, nil
}

func validateFrozenCandidate(candidate *frozenCandidate, removedLinksByInode map[string]uint64) error {
	if len(candidate.entries) == 0 {
		return errors.New("candidate manifest is empty")
	}
	if filepath.Dir(candidate.entries[0].path) != candidate.rootPath {
		return errors.New("candidate is no longer a direct child of its root")
	}
	expectedChildren := make(map[string]map[string]struct{})
	for _, entry := range candidate.entries {
		if !strictDescendant(candidate.rootPath, entry.path) {
			return fmt.Errorf("planned entry escaped root: %s", entry.display)
		}
		_, current, err := inspect(entry.path)
		if err != nil {
			return fmt.Errorf("%s: %w", entry.display, err)
		}
		if !entry.fingerprint.equalAfterRemovedLinks(current, removedLinkCount(removedLinksByInode, entry)) {
			return fmt.Errorf("%s fingerprint mismatch", entry.display)
		}
		if entry.path != candidate.entries[0].path {
			parent := filepath.Dir(entry.path)
			if expectedChildren[parent] == nil {
				expectedChildren[parent] = make(map[string]struct{})
			}
			expectedChildren[parent][filepath.Base(entry.path)] = struct{}{}
		}
	}
	for _, entry := range candidate.entries {
		if entry.kind != "directory" {
			continue
		}
		children, err := readDirNoFollow(entry.path, entry.fingerprint)
		if err != nil {
			return fmt.Errorf("%s: %w", entry.display, err)
		}
		expected := expectedChildren[entry.path]
		if len(children) != len(expected) {
			return fmt.Errorf("%s contents changed", entry.display)
		}
		for _, child := range children {
			if _, ok := expected[child.Name()]; !ok {
				return fmt.Errorf("%s contains an unplanned entry", entry.display)
			}
		}
	}
	return nil
}

func (e *Engine) removeCandidate(
	ctx context.Context,
	root frozenRoot,
	candidate *frozenCandidate,
	removedLinksByInode map[string]uint64,
) (removedAny, changed bool, err error) {
	rootHandle, err := os.OpenRoot(root.path)
	if err != nil {
		return false, true, fmt.Errorf("open compiled root: %w", err)
	}
	defer rootHandle.Close()
	rootInfo, currentRoot, err := inspectInRoot(rootHandle, ".")
	if err != nil || !rootInfo.IsDir() || !root.fingerprint.sameIdentity(currentRoot) {
		return false, true, errors.New("compiled root identity changed")
	}

	directories := make(map[string]fingerprint)
	for _, entry := range candidate.entries {
		if entry.kind == "directory" {
			directories[entry.path] = entry.fingerprint
		}
	}
	entries := append([]frozenEntry(nil), candidate.entries...)
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].depth == entries[j].depth {
			return entries[i].path > entries[j].path
		}
		return entries[i].depth > entries[j].depth
	})
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return removedAny, false, err
		}
		if !strictDescendant(root.path, entry.path) {
			return removedAny, true, errors.New("entry escaped compiled root")
		}

		parentPath := filepath.Dir(entry.path)
		parentRel, err := filepath.Rel(root.path, parentPath)
		if err != nil || parentRel == ".." || strings.HasPrefix(parentRel, ".."+string(filepath.Separator)) {
			return removedAny, true, errors.New("entry parent escaped compiled root")
		}
		expectedParent := root.fingerprint
		if parentRel != "." {
			var ok bool
			expectedParent, ok = directories[parentPath]
			if !ok {
				return removedAny, true, errors.New("entry has an unplanned parent")
			}
		}

		parentHandle := rootHandle
		closeParent := false
		if parentRel != "." {
			parentHandle, err = rootHandle.OpenRoot(parentRel)
			if err != nil {
				return removedAny, true, fmt.Errorf("open parent of %s: %w", entry.display, err)
			}
			closeParent = true
		}
		parentInfo, currentParent, inspectErr := inspectInRoot(parentHandle, ".")
		if inspectErr != nil || !parentInfo.IsDir() || !expectedParent.sameIdentity(currentParent) {
			if closeParent {
				_ = parentHandle.Close()
			}
			return removedAny, true, fmt.Errorf("parent of %s changed", entry.display)
		}
		_, current, inspectErr := inspectInRoot(parentHandle, filepath.Base(entry.path))
		if inspectErr != nil {
			if closeParent {
				_ = parentHandle.Close()
			}
			return removedAny, true, fmt.Errorf("revalidate %s: %w", entry.display, inspectErr)
		}
		if !entry.fingerprint.safeToRemoveNow(current, entry.kind, removedLinkCount(removedLinksByInode, entry)) {
			if closeParent {
				_ = parentHandle.Close()
			}
			return removedAny, true, fmt.Errorf("%s fingerprint mismatch", entry.display)
		}
		// Remove relative to a verified parent directory descriptor. This
		// prevents a concurrently swapped ancestor symlink from redirecting
		// deletion outside the compiled root.
		removeErr := parentHandle.Remove(filepath.Base(entry.path))
		if closeParent {
			_ = parentHandle.Close()
		}
		if removeErr != nil {
			return removedAny, false, fmt.Errorf("remove %s: %w", entry.display, removeErr)
		}
		recordRemovedLink(removedLinksByInode, entry)
		removedAny = true
	}
	return removedAny, false, nil
}

func removedLinkCount(removedLinksByInode map[string]uint64, entry frozenEntry) uint64 {
	if removedLinksByInode == nil || entry.kind == "directory" || !entry.fingerprint.HasIdentity {
		return 0
	}
	return removedLinksByInode[inodeKey(entry.path, entry.fingerprint)]
}

func recordRemovedLink(removedLinksByInode map[string]uint64, entry frozenEntry) {
	if removedLinksByInode == nil || entry.kind == "directory" || !entry.fingerprint.HasIdentity {
		return
	}
	key := inodeKey(entry.path, entry.fingerprint)
	removedLinksByInode[key]++
}

func rejectCandidate(result *CleanResult, callback Callback, candidate *frozenCandidate, reason string) {
	result.Rejected = append(result.Rejected, outcomeFor(candidate, reason))
	emit(callback, Event{
		Kind: EventCandidateRejected, RuleID: candidate.summary.RuleID,
		CandidateID: candidate.summary.ID, DisplayPath: candidate.summary.DisplayPath, Message: reason,
	})
}

func outcomeFor(candidate *frozenCandidate, reason string) CandidateOutcome {
	return CandidateOutcome{
		CandidateID: candidate.summary.ID, RuleID: candidate.summary.RuleID,
		DisplayPath: candidate.summary.DisplayPath, Reason: strings.TrimSpace(reason),
	}
}
