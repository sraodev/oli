package cleanup

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const ProtectionConfig = "Library/Application Support/Mac Cleanup Studio/exclusions.json"

var ErrProtection = errors.New("cleanup: exclusions are invalid, unreadable, or changed; repair the configuration before cleanup")

type protectionDocument struct {
	Version int      `json:"version"`
	Rules   []string `json:"rules"`
	Paths   []string `json:"paths"`
}

type protection struct {
	rules      map[string]bool
	paths      []string
	identities []fingerprint
}

// Exclusions are read-only policy, never a source of deletion roots. Missing
// configuration means no additional exclusions; malformed policy fails closed.
func (e *Engine) loadProtection() (*protection, error) {
	p := &protection{rules: map[string]bool{}}
	root, err := os.OpenRoot(e.home)
	if err != nil {
		return nil, ErrProtection
	}
	defer root.Close()
	_, current, err := inspectInRoot(root, ".")
	if err != nil || !e.homeFingerprint.sameIdentity(current) {
		return nil, ErrProtection
	}
	before, err := noSymlinkStat(root, ProtectionConfig)
	if os.IsNotExist(err) {
		return p, nil
	}
	if err != nil || !before.Mode().IsRegular() || before.Size() > 16<<10 {
		return nil, ErrProtection
	}
	f, err := root.Open(ProtectionConfig)
	if err != nil {
		return nil, ErrProtection
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(before, opened) {
		return nil, ErrProtection
	}
	var doc protectionDocument
	decoder := json.NewDecoder(io.LimitReader(f, 16<<10+1))
	start, err := decoder.Token()
	if err != nil || start != json.Delim('{') {
		return nil, ErrProtection
	}
	fields := map[string]bool{}
	for decoder.More() {
		token, err := decoder.Token()
		key, ok := token.(string)
		if err != nil || !ok || fields[key] {
			return nil, ErrProtection
		}
		fields[key] = true
		switch key {
		case "version":
			err = decoder.Decode(&doc.Version)
		case "rules":
			err = decoder.Decode(&doc.Rules)
		case "paths":
			err = decoder.Decode(&doc.Paths)
		default:
			return nil, ErrProtection
		}
		if err != nil {
			return nil, ErrProtection
		}
	}
	end, err := decoder.Token()
	if err != nil || end != json.Delim('}') || doc.Version != 1 || len(doc.Rules)+len(doc.Paths) > 128 {
		return nil, ErrProtection
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return nil, ErrProtection
	}
	after, err := noSymlinkStat(root, ProtectionConfig)
	if err != nil || !fingerprintFromInfo(before, "").equal(fingerprintFromInfo(after, "")) {
		return nil, ErrProtection
	}
	for _, id := range doc.Rules {
		if _, ok := e.byID[id]; !ok || p.rules[id] {
			return nil, ErrProtection
		}
		p.rules[id] = true
	}
	seen := map[string]bool{}
	for _, path := range doc.Paths {
		if !validProtectionPath(path) || seen[path] {
			return nil, ErrProtection
		}
		seen[path] = true
		p.paths = append(p.paths, filepath.Join(e.home, path))
		info, err := noSymlinkStat(root, path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, ErrProtection
		}
		p.identities = append(p.identities, fingerprintFromInfo(info, ""))
	}
	// Native filesystem identity also protects case/Unicode aliases of an
	// excluded ancestor above a compiled root; lexical matching alone cannot.
	if len(p.identities) > 0 {
		for _, rule := range e.rules {
			for _, compiled := range rule.roots {
				rel, err := filepath.Rel(e.home, compiled.path)
				if err != nil {
					return nil, ErrProtection
				}
				parts := strings.Split(rel, "/")
				for i := range parts {
					info, err := noSymlinkStat(root, strings.Join(parts[:i+1], "/"))
					if err != nil {
						break
					}
					fp := fingerprintFromInfo(info, "")
					for _, id := range p.identities {
						if id.HasIdentity && id.sameIdentity(fp) {
							p.paths = append(p.paths, compiled.path)
						}
					}
				}
			}
		}
	}
	return p, nil
}

func validProtectionPath(path string) bool {
	if path == "" || len(path) > 1024 || filepath.IsAbs(path) || strings.ContainsAny(path, "\x00\r\n") {
		return false
	}
	for _, part := range strings.Split(path, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}

func noSymlinkStat(root *os.Root, path string) (os.FileInfo, error) {
	var info os.FileInfo
	var err error
	parts := strings.Split(path, "/")
	for i := range parts {
		info, err = root.Lstat(strings.Join(parts[:i+1], "/"))
		if err != nil {
			return nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, ErrProtection
		}
	}
	return info, nil
}

func (p *protection) protects(candidate *frozenCandidate) bool {
	if p.rules[candidate.summary.RuleID] {
		return true
	}
	for _, entry := range candidate.entries {
		for _, path := range p.paths {
			if entry.path == path || strictDescendant(path, entry.path) || strictDescendant(entry.path, path) {
				return true
			}
		}
		for _, identity := range p.identities {
			if identity.HasIdentity && identity.sameIdentity(entry.fingerprint) {
				return true
			}
		}
	}
	return false
}
