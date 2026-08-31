package cleanup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// Exploration is deliberately separate from Scan: it cannot carry a deletion
// plan. It inspects metadata only, including in personal folders.
type Exploration struct {
	Action       Action        `json:"action"`
	StartedAt    time.Time     `json:"started_at"`
	CompletedAt  time.Time     `json:"completed_at"`
	Scope        string        `json:"scope"`
	MinSizeBytes int64         `json:"min_size_bytes"`
	OlderDays    int           `json:"older_than_days"`
	Limit        int           `json:"limit"`
	Entries      int           `json:"entries_inspected"`
	Partial      bool          `json:"partial"`
	Total        Metrics       `json:"total"`
	Scopes       []ScopeUsage  `json:"scopes"`
	Folders      []ExploreItem `json:"folders"`
	LargeFiles   []ExploreItem `json:"large_files"`
	OldFiles     []ExploreItem `json:"old_files"`
	Warnings     []Warning     `json:"warnings"`
}

type ExploreScope struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	DisplayPath string `json:"display_path"`
}

type ScopeUsage struct {
	ExploreScope
	Metrics Metrics `json:"metrics"`
}

type ExploreItem struct {
	DisplayPath string    `json:"display_path"`
	Kind        string    `json:"kind"`
	Metrics     Metrics   `json:"metrics"`
	Modified    time.Time `json:"modified"`
}

type ExploreOptions struct {
	Scope         string
	MinSizeBytes  int64
	OlderThanDays int
	Limit         int
	Now           time.Time
	// Internal test/resource bound; not exposed as a path or mutation option.
	entryLimit int
}

func ExplorationScopes() []ExploreScope {
	return []ExploreScope{
		{ID: "downloads", Name: "Downloads", DisplayPath: "~/Downloads"},
		{ID: "documents", Name: "Documents", DisplayPath: "~/Documents"},
		{ID: "desktop", Name: "Desktop", DisplayPath: "~/Desktop"},
		{ID: "movies", Name: "Movies", DisplayPath: "~/Movies"},
		{ID: "music", Name: "Music", DisplayPath: "~/Music"},
		{ID: "pictures", Name: "Pictures", DisplayPath: "~/Pictures"},
		{ID: "applications", Name: "User applications", DisplayPath: "~/Applications"},
	}
}

func ValidExploreScope(id string) bool {
	if id == "all" {
		return true
	}
	for _, scope := range ExplorationScopes() {
		if scope.ID == id {
			return true
		}
	}
	return false
}

func (e *Engine) Explore(ctx context.Context, opts ExploreOptions) (*Exploration, error) {
	if opts.Scope == "" {
		opts.Scope = "downloads"
	}
	if !ValidExploreScope(opts.Scope) {
		return nil, fmt.Errorf("unknown exploration scope %q", opts.Scope)
	}
	if opts.Limit == 0 {
		opts.Limit = 50
	}
	if opts.MinSizeBytes == 0 {
		opts.MinSizeBytes = 100 << 20
	}
	if opts.OlderThanDays == 0 {
		opts.OlderThanDays = 180
	}
	if opts.Limit < 1 || opts.Limit > 200 || opts.MinSizeBytes < 1 || opts.OlderThanDays < 1 || opts.OlderThanDays > 36500 {
		return nil, fmt.Errorf("invalid exploration limits")
	}
	if opts.entryLimit <= 0 {
		opts.entryLimit = 200000
	}
	if opts.Now.IsZero() {
		opts.Now = time.Now()
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	report := &Exploration{
		Action: ActionScanOnly, StartedAt: time.Now(), Scope: opts.Scope,
		MinSizeBytes: opts.MinSizeBytes, OlderDays: opts.OlderThanDays, Limit: opts.Limit,
		Scopes: []ScopeUsage{}, Folders: []ExploreItem{}, LargeFiles: []ExploreItem{}, OldFiles: []ExploreItem{}, Warnings: []Warning{},
	}
	home, err := os.OpenRoot(e.home)
	if err != nil {
		return nil, fmt.Errorf("open exploration home: %s", publicError(e.home, err))
	}
	defer home.Close()
	_, fp, err := inspectInRoot(home, ".")
	if err != nil || !e.homeFingerprint.sameIdentity(fp) {
		return nil, errors.New("exploration home identity changed")
	}
	walker := explorer{ctx: ctx, opts: opts, report: report, device: fp.Device, seen: make(map[string]bool)}
	for _, scope := range ExplorationScopes() {
		if opts.Scope != "all" && scope.ID != opts.Scope {
			continue
		}
		name := filepath.Base(scope.DisplayPath)
		info, scopeFP, err := inspectInRoot(home, name)
		if os.IsNotExist(err) {
			report.Scopes = append(report.Scopes, ScopeUsage{ExploreScope: scope})
			continue
		}
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			walker.warn(scope.DisplayPath, "scope_unavailable", "Scope is unavailable, not a directory, or a symlink.")
			continue
		}
		metrics, err := walker.directory(home, name, scope.DisplayPath, scopeFP, 0)
		if err != nil {
			return nil, err
		}
		report.Scopes = append(report.Scopes, ScopeUsage{ExploreScope: scope, Metrics: metrics})
		addMetrics(&report.Total, metrics)
		if walker.stopped {
			break
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	report.CompletedAt = time.Now()
	return report, nil
}

type explorer struct {
	ctx     context.Context
	opts    ExploreOptions
	report  *Exploration
	device  uint64
	seen    map[string]bool
	stopped bool
}

func (w *explorer) warn(path, code, message string) {
	w.report.Partial = true
	if len(w.report.Warnings) < 100 {
		w.report.Warnings = append(w.report.Warnings, Warning{DisplayPath: path, Code: code, Message: message})
	}
}

func (w *explorer) directory(parent *os.Root, name, display string, expected fingerprint, depth int) (Metrics, error) {
	var total Metrics
	if err := w.ctx.Err(); err != nil {
		return total, err
	}
	if w.stopped {
		return total, nil
	}
	if depth > 64 || expected.Device != w.device {
		w.warn(display, "boundary", "Skipped a depth or filesystem boundary.")
		return total, nil
	}
	root, err := parent.OpenRoot(name)
	if err != nil {
		w.warn(display, "directory_unavailable", "Directory could not be opened; permissions were not bypassed.")
		return total, nil
	}
	defer root.Close()
	_, actual, err := inspectInRoot(root, ".")
	if err != nil {
		w.warn(display, "directory_unverifiable", "Directory identity could not be checked; permissions were not bypassed.")
		return total, nil
	}
	if !expected.sameIdentity(actual) {
		w.warn(display, "directory_changed", "Directory changed while opening; skipped.")
		return total, nil
	}
	key := inodeKey(display, actual)
	if w.seen[key] {
		return total, nil
	}
	w.seen[key] = true
	file, err := root.Open(".")
	if err != nil {
		w.warn(display, "directory_unreadable", "Directory contents could not be listed.")
		return total, nil
	}
	defer file.Close()
	for !w.stopped {
		entries, readErr := file.ReadDir(128)
		for _, entry := range entries {
			if err := w.ctx.Err(); err != nil {
				return total, err
			}
			if w.report.Entries >= w.opts.entryLimit {
				w.stopped = true
				w.warn(display, "entry_limit", "Entry limit reached; choose a narrower scope for a more complete report.")
				break
			}
			w.report.Entries++
			path := display + "/" + entry.Name()
			info, err := root.Lstat(entry.Name())
			if err != nil {
				w.warn(path, "metadata_unavailable", "File metadata could not be read.")
				continue
			}
			if info.Mode()&os.ModeSymlink != 0 {
				total.Symlinks++
				total.Items++
				continue
			}
			fp := fingerprintFromInfo(info, "")
			if fp.Device != w.device {
				w.warn(path, "filesystem_boundary", "Skipped a different filesystem.")
				continue
			}
			if info.IsDir() {
				metrics, err := w.directory(root, entry.Name(), path, fp, depth+1)
				if err != nil {
					return total, err
				}
				addMetrics(&total, metrics)
				total.Directories++
				total.Items++
				if depth == 0 {
					w.report.Folders = topExploreItems(w.report.Folders, ExploreItem{DisplayPath: path, Kind: "directory", Metrics: metrics, Modified: info.ModTime()}, w.opts.Limit)
				}
				continue
			}
			if !info.Mode().IsRegular() {
				continue
			}
			key := inodeKey(path, fp)
			if w.seen[key] {
				continue
			}
			w.seen[key] = true
			metrics := Metrics{Items: 1, Files: 1, UniqueFiles: 1, LogicalBytes: info.Size(), AllocatedBytes: allocatedBytes(fp.Blocks), LatestModified: info.ModTime()}
			addMetrics(&total, metrics)
			item := ExploreItem{DisplayPath: path, Kind: "file", Metrics: metrics, Modified: info.ModTime()}
			if info.Size() >= w.opts.MinSizeBytes {
				w.report.LargeFiles = topExploreItems(w.report.LargeFiles, item, w.opts.Limit)
			}
			if !info.ModTime().After(w.opts.Now.AddDate(0, 0, -w.opts.OlderThanDays)) {
				w.report.OldFiles = topExploreItems(w.report.OldFiles, item, w.opts.Limit)
			}
		}
		if readErr != nil {
			if !errors.Is(readErr, io.EOF) {
				w.warn(display, "directory_incomplete", "Directory listing was incomplete.")
			}
			break
		}
	}
	return total, nil
}

func topExploreItems(items []ExploreItem, item ExploreItem, limit int) []ExploreItem {
	items = append(items, item)
	sort.Slice(items, func(i, j int) bool {
		if items[i].Metrics.LogicalBytes == items[j].Metrics.LogicalBytes {
			return items[i].DisplayPath < items[j].DisplayPath
		}
		return items[i].Metrics.LogicalBytes > items[j].Metrics.LogicalBytes
	})
	if len(items) > limit {
		items = items[:limit]
	}
	return items
}
