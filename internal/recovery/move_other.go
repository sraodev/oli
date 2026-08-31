//go:build !darwin

package recovery

import "os"

func checkMoveParents(_, _ *os.File) error                             { return ErrUnsupportedVolume }
func renameExclusive(_ *os.File, _ string, _ *os.File, _ string) error { return ErrUnsupportedVolume }
