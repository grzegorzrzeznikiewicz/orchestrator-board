//go:build darwin

// Modified for Orchestrator Board by Gama Software, 2026 (OB-1).

package localsnapshot

import "syscall"

// canFilterReadChmod limits access-time-only CHMOD handling to the filesystem
// whose native notification and precise ctime behavior have been verified.
// Check the file's filesystem, since a repository may contain nested mounts.
func canFilterReadChmod(path string) bool {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return false
	}
	var name []rune
	for _, c := range stat.Fstypename {
		if c == 0 {
			break
		}
		name = append(name, rune(c))
	}
	return string(name) == "apfs"
}
