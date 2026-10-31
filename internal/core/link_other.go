//go:build !windows

package core

import "os"

// symlinkDir creates link as a symlink pointing at the directory target.
func symlinkDir(target, link string) error {
	return os.Symlink(target, link)
}
