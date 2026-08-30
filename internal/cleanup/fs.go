package cleanup

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type fingerprint struct {
	Device       uint64
	Inode        uint64
	Mode         uint32
	Size         int64
	ModifiedNano int64
	ChangedNano  int64
	Links        uint64
	Blocks       int64
	LinkTarget   string
	HasIdentity  bool
}

type frozenEntry struct {
	path        string
	display     string
	kind        string
	depth       int
	fingerprint fingerprint
}

type frozenRoot struct {
	path        string
	display     string
	fingerprint fingerprint
}

type frozenCandidate struct {
	summary  Candidate
	rootPath string
	entries  []frozenEntry
}

type frozenRule struct {
	info       RuleInfo
	action     Action
	auto       bool
	candidates []*frozenCandidate
	roots      map[string]frozenRoot
}

type frozenPlan struct {
	engineToken [32]byte
	scanID      string
	rules       map[string]*frozenRule
	order       []string
}

type platformFileStat struct {
	device      uint64
	inode       uint64
	links       uint64
	blocks      int64
	changedNano int64
	valid       bool
}

func inspect(path string) (fs.FileInfo, fingerprint, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fingerprint{}, err
	}
	fp, err := fingerprintInfo(path, info)
	return info, fp, err
}

func fingerprintInfo(path string, info fs.FileInfo) (fingerprint, error) {
	linkTarget := ""
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(path)
		if err != nil {
			return fingerprint{}, err
		}
		linkTarget = target
	}
	return fingerprintFromInfo(info, linkTarget), nil
}

// inspectInRoot resolves name through a traversal-resistant os.Root. It is
// used immediately before deletion so an ancestor symlink or rename cannot
// redirect metadata checks outside the compiled rule root.
func inspectInRoot(root *os.Root, name string) (fs.FileInfo, fingerprint, error) {
	info, err := root.Lstat(name)
	if err != nil {
		return nil, fingerprint{}, err
	}
	linkTarget := ""
	if info.Mode()&os.ModeSymlink != 0 {
		linkTarget, err = root.Readlink(name)
		if err != nil {
			return nil, fingerprint{}, err
		}
	}
	return info, fingerprintFromInfo(info, linkTarget), nil
}

func fingerprintFromInfo(info fs.FileInfo, linkTarget string) fingerprint {
	stat := fileStat(info)
	return fingerprint{
		Device: stat.device, Inode: stat.inode, Mode: uint32(info.Mode()),
		Size: info.Size(), ModifiedNano: info.ModTime().UnixNano(),
		ChangedNano: stat.changedNano, Links: stat.links, Blocks: stat.blocks,
		LinkTarget: linkTarget, HasIdentity: stat.valid,
	}
}

func (a fingerprint) equal(b fingerprint) bool {
	return a.Device == b.Device && a.Inode == b.Inode && a.Mode == b.Mode &&
		a.Size == b.Size && a.ModifiedNano == b.ModifiedNano &&
		a.ChangedNano == b.ChangedNano && a.Links == b.Links &&
		a.Blocks == b.Blocks && a.LinkTarget == b.LinkTarget &&
		a.HasIdentity == b.HasIdentity
}

func (a fingerprint) sameIdentity(b fingerprint) bool {
	if a.HasIdentity && b.HasIdentity {
		return a.Device == b.Device && a.Inode == b.Inode && a.Mode == b.Mode
	}
	return a.Mode == b.Mode
}

func (a fingerprint) equalAfterRemovedLinks(b fingerprint, removedLinks uint64) bool {
	if removedLinks == 0 {
		return a.equal(b)
	}
	if !a.HasIdentity || !b.HasIdentity || removedLinks >= a.Links {
		return false
	}
	if b.Links != a.Links-removedLinks {
		return false
	}
	// A successful unlink of another planned name for this inode changes only
	// its ctime and link count. Compare every other frozen field exactly.
	a.ChangedNano = b.ChangedNano
	a.Links = b.Links
	return a.equal(b)
}

func (a fingerprint) safeToRemoveNow(b fingerprint, kind string, removedLinks uint64) bool {
	if kind == "directory" {
		// Removing planned children changes directory metadata. Identity and an
		// empty-directory Root.Remove are the final safeguards at this point.
		return a.sameIdentity(b)
	}
	return a.equalAfterRemovedLinks(b, removedLinks)
}

func fileKind(mode fs.FileMode) string {
	switch {
	case mode&os.ModeSymlink != 0:
		return "symlink"
	case mode.IsDir():
		return "directory"
	case mode.IsRegular():
		return "file"
	default:
		return "other"
	}
}

func inodeKey(path string, fp fingerprint) string {
	if fp.HasIdentity {
		return fmt.Sprintf("%d:%d", fp.Device, fp.Inode)
	}
	return "path:" + path
}

func allocatedBytes(blocks int64) int64 {
	if blocks <= 0 {
		return 0
	}
	if blocks > math.MaxInt64/512 {
		return math.MaxInt64
	}
	return blocks * 512
}

func addInt64(a, b int64) int64 {
	if b > 0 && a > math.MaxInt64-b {
		return math.MaxInt64
	}
	if b < 0 && a < math.MinInt64-b {
		return math.MinInt64
	}
	return a + b
}

func addMetrics(dst *Metrics, src Metrics) {
	dst.LogicalBytes = addInt64(dst.LogicalBytes, src.LogicalBytes)
	dst.AllocatedBytes = addInt64(dst.AllocatedBytes, src.AllocatedBytes)
	dst.Items = addInt64(dst.Items, src.Items)
	dst.Files = addInt64(dst.Files, src.Files)
	dst.UniqueFiles = addInt64(dst.UniqueFiles, src.UniqueFiles)
	dst.Directories = addInt64(dst.Directories, src.Directories)
	dst.Symlinks = addInt64(dst.Symlinks, src.Symlinks)
	if src.LatestModified.After(dst.LatestModified) {
		dst.LatestModified = src.LatestModified
	}
}

func countEntry(metrics *Metrics, entry frozenEntry) {
	metrics.Items++
	modified := time.Unix(0, entry.fingerprint.ModifiedNano)
	if modified.After(metrics.LatestModified) {
		metrics.LatestModified = modified
	}
	switch entry.kind {
	case "file":
		metrics.Files++
	case "directory":
		metrics.Directories++
	case "symlink":
		metrics.Symlinks++
	}
}

func applyUniqueBytes(candidate *frozenCandidate, seen map[string]struct{}) {
	for _, entry := range candidate.entries {
		if entry.kind != "file" {
			continue
		}
		key := inodeKey(entry.path, entry.fingerprint)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		candidate.summary.Metrics.UniqueFiles++
		if entry.fingerprint.Size > 0 {
			candidate.summary.Metrics.LogicalBytes = addInt64(candidate.summary.Metrics.LogicalBytes, entry.fingerprint.Size)
		}
		candidate.summary.Metrics.AllocatedBytes = addInt64(candidate.summary.Metrics.AllocatedBytes, allocatedBytes(entry.fingerprint.Blocks))
	}
}

func walkCandidate(ctx context.Context, home, rootPath, path string, rootDevice uint64) (*frozenCandidate, error) {
	var entries []frozenEntry
	var metrics Metrics
	var walk func(string) error
	walk = func(current string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		info, fp, err := inspect(current)
		if err != nil {
			return err
		}
		kind := fileKind(info.Mode())
		if kind == "directory" && fp.HasIdentity && fp.Device != rootDevice {
			return fmt.Errorf("crosses a filesystem boundary")
		}
		rel, err := filepath.Rel(rootPath, current)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return fmt.Errorf("entry escaped its compiled root")
		}
		entry := frozenEntry{
			path: current, display: displayPath(home, current), kind: kind,
			depth: strings.Count(filepath.Clean(current), string(filepath.Separator)), fingerprint: fp,
		}
		entries = append(entries, entry)
		countEntry(&metrics, entry)
		if kind != "directory" {
			return nil
		}

		children, err := readDirNoFollow(current, fp)
		if err != nil {
			return err
		}
		sort.Slice(children, func(i, j int) bool { return children[i].Name() < children[j].Name() })
		for _, child := range children {
			if err := walk(filepath.Join(current, child.Name())); err != nil {
				return err
			}
		}
		_, after, err := inspect(current)
		if err != nil {
			return err
		}
		if !fp.equal(after) {
			return fmt.Errorf("directory changed while it was scanned")
		}
		return nil
	}
	if err := walk(path); err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, errors.New("empty candidate manifest")
	}
	return &frozenCandidate{rootPath: rootPath, entries: entries, summary: Candidate{Metrics: metrics}}, nil
}

func manifestDigest(home, ruleID string, entries []frozenEntry) string {
	h := sha256.New()
	h.Write([]byte(ruleID))
	h.Write([]byte{0})
	var buf [8]byte
	for _, entry := range entries {
		h.Write([]byte(displayPath(home, entry.path)))
		h.Write([]byte{0})
		binary.LittleEndian.PutUint64(buf[:], entry.fingerprint.Device)
		h.Write(buf[:])
		binary.LittleEndian.PutUint64(buf[:], entry.fingerprint.Inode)
		h.Write(buf[:])
		binary.LittleEndian.PutUint64(buf[:], uint64(entry.fingerprint.Mode))
		h.Write(buf[:])
		binary.LittleEndian.PutUint64(buf[:], uint64(entry.fingerprint.Size))
		h.Write(buf[:])
		binary.LittleEndian.PutUint64(buf[:], uint64(entry.fingerprint.ModifiedNano))
		h.Write(buf[:])
		binary.LittleEndian.PutUint64(buf[:], uint64(entry.fingerprint.ChangedNano))
		h.Write(buf[:])
		binary.LittleEndian.PutUint64(buf[:], entry.fingerprint.Links)
		h.Write(buf[:])
		binary.LittleEndian.PutUint64(buf[:], uint64(entry.fingerprint.Blocks))
		h.Write(buf[:])
		h.Write([]byte(entry.fingerprint.LinkTarget))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func newScanID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}

func sortDirEntries(entries []os.DirEntry) {
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
}

func publicError(home string, err error) string {
	if err == nil {
		return ""
	}
	return strings.ReplaceAll(err.Error(), filepath.Clean(home), "~")
}
