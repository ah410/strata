//go:build linux || darwin

package fileops

import (
	"golang.org/x/sys/unix"

	"strata/internal/safepath"
)

// Usage reports filesystem capacity for the volume backing root. The numbers
// come from statfs, so they reflect the real ZFS dataset on TrueNAS rather
// than anything the container knows about.
func Usage(root *safepath.Root) (*DiskUsage, error) {
	var st unix.Statfs_t
	if err := unix.Statfs(root.Path(), &st); err != nil {
		return nil, err
	}
	bs := uint64(st.Bsize)
	total := st.Blocks * bs
	// Bavail excludes blocks reserved for root, which is what a non-root
	// process can actually write.
	free := st.Bavail * bs
	return &DiskUsage{
		Total: total,
		Free:  free,
		Used:  total - free,
	}, nil
}
