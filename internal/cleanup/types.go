package cleanup

import (
	"errors"
	"time"
)

// Risk describes how much judgment a cleanup rule requires.
type Risk string

const (
	RiskSafe    Risk = "safe"
	RiskCaution Risk = "caution"
	RiskReview  Risk = "review"
)

// Action describes whether a rule can delete data or is informational only.
type Action string

const (
	ActionClean    Action = "clean"
	ActionScanOnly Action = "scan_only"
)

// Rule is a compiled, user-scoped cleanup rule. Roots are never serialized so
// API responses do not disclose the user's full home path.
type Rule struct {
	ID          string        `json:"id"`
	Name        string        `json:"name"`
	Description string        `json:"description,omitempty"`
	Risk        Risk          `json:"risk"`
	Action      Action        `json:"action"`
	Default     bool          `json:"default"`
	Auto        bool          `json:"auto"`
	MinimumAge  time.Duration `json:"-"`
	Roots       []string      `json:"-"`
}

// RuleInfo is the JSON-safe form of a Rule used by clients.
type RuleInfo struct {
	ID                string   `json:"id"`
	Name              string   `json:"name"`
	Description       string   `json:"description,omitempty"`
	Risk              Risk     `json:"risk"`
	Action            Action   `json:"action"`
	Default           bool     `json:"default"`
	Auto              bool     `json:"auto"`
	MinimumAgeSeconds int64    `json:"minimum_age_seconds"`
	Roots             []string `json:"roots"`
}

// Metrics describe a set of planned filesystem entries. Logical and allocated
// bytes count each hard-linked inode once.
type Metrics struct {
	LogicalBytes   int64     `json:"logical_bytes"`
	AllocatedBytes int64     `json:"allocated_bytes"`
	Items          int64     `json:"items"`
	Files          int64     `json:"files"`
	UniqueFiles    int64     `json:"unique_files"`
	Directories    int64     `json:"directories"`
	Symlinks       int64     `json:"symlinks"`
	LatestModified time.Time `json:"latest_modified,omitempty"`
}

// Candidate is one direct child of a rule root. Fingerprint identifies the
// private frozen manifest used during cleanup; it is not a content hash.
type Candidate struct {
	Risk              Risk      `json:"risk"`
	ID                string    `json:"id"`
	RuleID            string    `json:"rule_id"`
	DisplayPath       string    `json:"display_path"`
	RootDisplayPath   string    `json:"root_display_path"`
	Kind              string    `json:"kind"`
	Fingerprint       string    `json:"fingerprint"`
	Modified          time.Time `json:"modified"`
	LatestModified    time.Time `json:"latest_modified"`
	Eligible          bool      `json:"eligible"`
	AutoEligible      bool      `json:"auto_eligible"`
	EligibilityReason string    `json:"eligibility_reason,omitempty"`
	Metrics           Metrics   `json:"metrics"`
}

// Warning is a non-fatal condition encountered during scanning.
type Warning struct {
	RuleID      string `json:"rule_id,omitempty"`
	DisplayPath string `json:"display_path,omitempty"`
	Code        string `json:"code"`
	Message     string `json:"message"`
}

// RuleScan contains the public data for a scanned rule.
type RuleScan struct {
	Rule       RuleInfo    `json:"rule"`
	Candidates []Candidate `json:"candidates"`
	Detected   Metrics     `json:"detected"`
	Eligible   Metrics     `json:"eligible"`
	Warnings   []Warning   `json:"warnings,omitempty"`
}

// Scan is a read-only cleanup proposal. Its exported data can be freely used
// by clients; Clean relies only on the private frozen plan created with it.
type Scan struct {
	Partial         bool       `json:"partial"`
	WarningsOmitted int        `json:"warnings_omitted"`
	ID              string     `json:"id"`
	StartedAt       time.Time  `json:"started_at"`
	CompletedAt     time.Time  `json:"completed_at"`
	Rules           []RuleScan `json:"rules"`
	Detected        Metrics    `json:"detected"`
	Eligible        Metrics    `json:"eligible"`
	Warnings        []Warning  `json:"warnings,omitempty"`
	plan            *frozenPlan
}

// ReleaseScanOnlyManifests drops recursive private manifests for report-only
// rules while preserving their public scan results. The small action stubs stay
// in the plan so attempts to clean those rules still fail with ErrScanOnly.
// Cleanup-capable rule manifests are not changed.
func (s *Scan) ReleaseScanOnlyManifests() {
	if s == nil || s.plan == nil {
		return
	}
	s.plan.mu.Lock()
	defer s.plan.mu.Unlock()
	for _, rule := range s.plan.rules {
		if rule.action != ActionScanOnly {
			continue
		}
		rule.candidates = nil
		rule.roots = nil
	}
}

// EventKind identifies streaming scan and cleanup updates.
type EventKind string

const (
	EventScanStarted       EventKind = "scan_started"
	EventRuleStarted       EventKind = "rule_started"
	EventCandidateScanned  EventKind = "candidate_scanned"
	EventWarning           EventKind = "warning"
	EventRuleCompleted     EventKind = "rule_completed"
	EventScanCompleted     EventKind = "scan_completed"
	EventCleanStarted      EventKind = "clean_started"
	EventCandidateRemoved  EventKind = "candidate_removed"
	EventCandidateRejected EventKind = "candidate_rejected"
	EventCandidateSkipped  EventKind = "candidate_skipped"
	EventCandidateFailed   EventKind = "candidate_failed"
	EventCleanCompleted    EventKind = "clean_completed"
)

// Event is emitted synchronously as work progresses.
type Event struct {
	Kind        EventKind     `json:"kind"`
	RuleID      string        `json:"rule_id,omitempty"`
	CandidateID string        `json:"candidate_id,omitempty"`
	DisplayPath string        `json:"display_path,omitempty"`
	Message     string        `json:"message,omitempty"`
	Candidate   *Candidate    `json:"candidate,omitempty"`
	Rule        *RuleScan     `json:"rule,omitempty"`
	Warning     *Warning      `json:"warning,omitempty"`
	Failure     *CleanFailure `json:"failure,omitempty"`
}

type Callback func(Event)

type ScanOptions struct {
	entryLimit     int
	candidateLimit int
	RuleIDs        []string  `json:"rule_ids,omitempty"`
	Callback       Callback  `json:"-"`
	Now            time.Time `json:"-"`
}

type CleanOptions struct {
	RuleIDs []string `json:"rule_ids,omitempty"`
	// A non-nil CandidateIDs selects exact scan-bound candidates instead of rules.
	// An empty explicit selection is an error, never a request for all candidates.
	CandidateIDs []string `json:"candidate_ids,omitempty"`
	Callback     Callback `json:"-"`
}

// Selection is a detached preview, never deletion authority.
type Selection struct {
	ScanID       string      `json:"scan_id"`
	RuleIDs      []string    `json:"rule_ids"`
	CandidateIDs []string    `json:"candidate_ids"`
	Candidates   []Candidate `json:"candidates"`
	Metrics      Metrics     `json:"metrics"`
}

// CandidateOutcome records a candidate that was removed, skipped, or rejected.
type CandidateOutcome struct {
	CandidateID string `json:"candidate_id"`
	RuleID      string `json:"rule_id"`
	DisplayPath string `json:"display_path"`
	Reason      string `json:"reason,omitempty"`
}

// CleanFailure records an unexpected error after cleanup mutation began. A
// changed preflight entry is reported as Rejected instead.
type CleanFailure struct {
	CandidateID string `json:"candidate_id"`
	RuleID      string `json:"rule_id"`
	DisplayPath string `json:"display_path"`
	Message     string `json:"message"`
}

type CleanResult struct {
	ScanID      string             `json:"scan_id"`
	StartedAt   time.Time          `json:"started_at"`
	CompletedAt time.Time          `json:"completed_at"`
	RuleIDs     []string           `json:"rule_ids"`
	Removed     []CandidateOutcome `json:"removed,omitempty"`
	Skipped     []CandidateOutcome `json:"skipped,omitempty"`
	Rejected    []CandidateOutcome `json:"rejected,omitempty"`
	Failures    []CleanFailure     `json:"failures,omitempty"`
	Freed       Metrics            `json:"freed"`
}

type VolumeInfo struct {
	Path           string `json:"path"`
	TotalBytes     int64  `json:"total_bytes"`
	UsedBytes      int64  `json:"used_bytes"`
	FreeBytes      int64  `json:"free_bytes"`
	AvailableBytes int64  `json:"available_bytes"`
}

var (
	ErrInvalidScan     = errors.New("cleanup: scan was not created by this engine")
	ErrNoRulesSelected = errors.New("cleanup: no rules selected")
	ErrScanOnly        = errors.New("cleanup: scan-only rule cannot be cleaned")
)
