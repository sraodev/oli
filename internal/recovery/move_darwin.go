//go:build darwin

package recovery

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

func checkMoveParents(source, destination *os.File) error {
	var device uint64
	for i, parent := range []*os.File{source, destination} {
		var st unix.Stat_t
		if err := unix.Fstat(int(parent.Fd()), &st); err != nil {
			return err
		}
		if st.Mode&unix.S_IFMT != unix.S_IFDIR {
			return errors.New("recovery: parent is not a directory")
		}
		if i == 0 {
			device = uint64(st.Dev)
		} else if device != uint64(st.Dev) {
			return ErrCrossVolume
		}
		var volume unix.Statfs_t
		if err := unix.Fstatfs(int(parent.Fd()), &volume); err != nil {
			return err
		}
		if unix.ByteSliceToString(volume.Fstypename[:]) != "apfs" || volume.Flags&unix.MNT_LOCAL == 0 || volume.Flags&unix.MNT_RDONLY != 0 {
			return ErrUnsupportedVolume
		}
	}
	return nil
}

func renameExclusive(source *os.File, name string, destination *os.File, target string) error {
	return unix.RenameatxNp(int(source.Fd()), name, int(destination.Fd()), target, unix.RENAME_EXCL)
}
