//go:build !darwin && !linux

package cleanup

import (
	"fmt"
	"io/fs"
)

func fileStat(info fs.FileInfo) platformFileStat {
	return platformFileStat{}
}

func VolumeStats(path string) (VolumeInfo, error) {
	return VolumeInfo{}, fmt.Errorf("cleanup: volume statistics are unsupported on this platform")
}
