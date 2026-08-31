//go:build darwin || linux

package cleanup

import (
	"fmt"
	"io"
	"os"
	"syscall"
)

func readDirNoFollow(path string, expected fingerprint) ([]os.DirEntry, error) {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		syscall.Close(fd)
		return nil, fmt.Errorf("cleanup: open directory %q", path)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	actual, err := fingerprintInfo(path, info)
	if err != nil {
		return nil, err
	}
	if !expected.equal(actual) {
		return nil, fmt.Errorf("directory changed before it was read")
	}
	entries, err := file.ReadDir(200001)
	if err != nil && err != io.EOF {
		return nil, err
	}
	if len(entries) > 200000 {
		return nil, ErrScanLimit
	}
	_, after, err := inspect(path)
	if err != nil {
		return nil, err
	}
	if !expected.equal(after) {
		return nil, fmt.Errorf("directory changed while it was read")
	}
	return entries, nil
}
