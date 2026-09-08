package cleanup

import (
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
)

// Engine scans and cleans only the rules compiled for one home directory.
// It does not elevate privileges or invoke external commands.
type Engine struct {
	home            string
	homeFingerprint fingerprint
	rules           []compiledRule
	byID            map[string]int
	token           [32]byte
}

func New(home string, rules []Rule) (*Engine, error) {
	if home == "" {
		return nil, fmt.Errorf("cleanup: home path is required")
	}
	absHome, err := filepath.Abs(home)
	if err != nil {
		return nil, fmt.Errorf("cleanup: resolve home: %w", err)
	}
	absHome = filepath.Clean(absHome)
	if filepath.Dir(absHome) == absHome {
		return nil, fmt.Errorf("cleanup: filesystem root cannot be used as home")
	}
	info, err := os.Lstat(absHome)
	if err != nil {
		return nil, fmt.Errorf("cleanup: inspect home: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return nil, fmt.Errorf("cleanup: home must be a real directory, not a symlink")
	}
	homeFingerprint, err := fingerprintInfo(absHome, info)
	if err != nil {
		return nil, fmt.Errorf("cleanup: fingerprint home: %w", err)
	}

	compiled, err := compileRules(absHome, rules)
	if err != nil {
		return nil, err
	}
	if len(compiled) == 0 {
		return nil, fmt.Errorf("cleanup: at least one rule is required")
	}
	e := &Engine{
		home: absHome, homeFingerprint: homeFingerprint,
		rules: compiled, byID: make(map[string]int, len(compiled)),
	}
	for i := range compiled {
		e.byID[compiled[i].rule.ID] = i
	}
	if _, err := rand.Read(e.token[:]); err != nil {
		return nil, fmt.Errorf("cleanup: initialize engine identity: %w", err)
	}
	return e, nil
}

func NewDefault(home string) (*Engine, error) {
	return New(home, DefaultRules(home))
}

// Rules returns detached JSON-safe rule descriptions.
func (e *Engine) Rules() []RuleInfo {
	infos := make([]RuleInfo, len(e.rules))
	for i := range e.rules {
		infos[i] = cloneRuleInfo(e.rules[i].info)
	}
	return infos
}

func cloneRuleInfo(info RuleInfo) RuleInfo {
	info.Roots = append([]string(nil), info.Roots...)
	info.ExcludedChildren = append([]string(nil), info.ExcludedChildren...)
	return info
}

func (e *Engine) selectedRules(ids []string, emptyMeansAll bool) ([]compiledRule, error) {
	if len(ids) == 0 {
		if !emptyMeansAll {
			return nil, ErrNoRulesSelected
		}
		out := make([]compiledRule, len(e.rules))
		copy(out, e.rules)
		return out, nil
	}
	wanted := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if _, duplicate := wanted[id]; duplicate {
			continue
		}
		if _, ok := e.byID[id]; !ok {
			return nil, fmt.Errorf("cleanup: unknown rule %q", id)
		}
		wanted[id] = struct{}{}
	}
	out := make([]compiledRule, 0, len(wanted))
	for _, rule := range e.rules {
		if _, ok := wanted[rule.rule.ID]; ok {
			out = append(out, rule)
		}
	}
	return out, nil
}

func emit(callback Callback, event Event) {
	if callback != nil {
		callback(event)
	}
}
