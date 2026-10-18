// SPDX-License-Identifier: Apache-2.0

//go:build !unix

package harness

import "os"

// Inode returns 0 where file info carries no inode (Windows); cursors then
// rely on size and modification time.
func Inode(os.FileInfo) uint64 { return 0 }
