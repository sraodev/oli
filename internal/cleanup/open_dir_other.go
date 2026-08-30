//go:build !darwin && !linux

package cleanup

import "os"

func readDirNoFollow(path string, expected fingerprint) ([]os.DirEntry, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	entries, err := file.ReadDir(-1)
	if err != nil {
		return nil, err
	}
	_, after, err := inspect(path)
	if err != nil {
		return nil, err
	}
	if !expected.equal(after) {
		return nil, &os.PathError{Op: "revalidate", Path: path, Err: os.ErrInvalid}
	}
	return entries, nil
}
