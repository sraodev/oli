// Package recovery contains the platform proof for recoverable cleanup.
// It is not wired to cleanup, CLI, or dashboard mutation commands yet.
package recovery

import (
	"errors"
	"os"
	"strings"
)

var (
	ErrUnsupportedVolume = errors.New("recovery: requires writable local APFS")
	ErrCrossVolume       = errors.New("recovery: cross-volume moves are not supported")
)

// moveOutcome distinguishes an unsuccessful rename from a rename whose
// durability could not be confirmed. A caller must never blindly retry the latter.
type moveOutcome struct {
	Moved   bool
	Durable bool
}

// Parent handles must already have been verified against the caller's private
// plan. This primitive never accepts paths, resolves parents, or grants authority.
func exclusiveMove(source *os.File, name string, destination *os.File, target string) (moveOutcome, error) {
	return exclusiveMoveWithSync(source, name, destination, target, (*os.File).Sync)
}

func exclusiveMoveWithSync(source *os.File, name string, destination *os.File, target string, syncDir func(*os.File) error) (moveOutcome, error) {
	if source == nil || destination == nil || !component(name) || !component(target) {
		return moveOutcome{}, errors.New("recovery: move requires directory handles and single entry names")
	}
	if err := checkMoveParents(source, destination); err != nil {
		return moveOutcome{}, err
	}
	if err := renameExclusive(source, name, destination, target); err != nil {
		return moveOutcome{}, err
	}
	result := moveOutcome{Moved: true}
	// Attempt both syncs even if the first fails; the journal must remain pending
	// on any error. No fallback rename, rollback, or copy/delete is safe here.
	err := errors.Join(syncDir(source), syncDir(destination))
	result.Durable = err == nil
	return result, err
}

func component(name string) bool {
	return name != "" && name != "." && name != ".." && !strings.ContainsAny(name, "/\x00")
}
