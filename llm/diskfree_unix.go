//go:build !windows

package llm

import "golang.org/x/sys/unix"

// diskFree returns the bytes available to this user on path's filesystem.
func diskFree(path string) (uint64, error) {
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return 0, err
	}
	return st.Bavail * uint64(st.Bsize), nil
}
