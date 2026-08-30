//go:build darwin

package cleanup

import (
	"io/fs"
	"math"
	"syscall"
)

func fileStat(info fs.FileInfo) platformFileStat {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return platformFileStat{}
	}
	return platformFileStat{
		device: uint64(st.Dev), inode: st.Ino, links: uint64(st.Nlink),
		blocks: st.Blocks, changedNano: st.Ctimespec.Sec*1e9 + int64(st.Ctimespec.Nsec), valid: true,
	}
}

func VolumeStats(path string) (VolumeInfo, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return VolumeInfo{}, err
	}
	blockSize := uint64(stat.Bsize)
	total := clampVolume(blockSize, stat.Blocks)
	free := clampVolume(blockSize, stat.Bfree)
	available := clampVolume(blockSize, stat.Bavail)
	return VolumeInfo{
		Path: path, TotalBytes: total, UsedBytes: total - free,
		FreeBytes: free, AvailableBytes: available,
	}, nil
}

func clampVolume(a, b uint64) int64 {
	if a != 0 && b > math.MaxInt64/a {
		return math.MaxInt64
	}
	return int64(a * b)
}
