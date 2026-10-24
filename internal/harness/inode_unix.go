// SPDX-License-Identifier: SUL-1.0

//go:build unix

package harness

import (
	"os"
	"syscall"
)

// Inode returns the file's inode number, used by cursors to notice a file
// that was replaced under the same name.
func Inode(info os.FileInfo) uint64 {
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		return uint64(st.Ino) //nolint:unconvert // Ino is uint32 on some platforms
	}
	return 0
}
