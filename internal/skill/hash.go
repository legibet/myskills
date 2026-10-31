package skill

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Hash returns the content hash of dir as "v1:<hex sha256>" (design §4).
//
// Entries below dir (not dir itself) are hashed in lexical order of their
// slash-separated relative path, skipping .git and .DS_Store at any depth.
// Each entry writes a kind byte ('F', 'D' or 'L') and its length-prefixed path;
// a file then writes an executable flag byte and its length-prefixed content, a
// symlink its length-prefixed target. Lengths are uint64 big-endian. Symlinks
// inside dir are not followed; dir itself may be a symlink.
func Hash(dir string) (string, error) {
	root, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", err
	}

	type entry struct {
		rel, path string
		mode      fs.FileMode
	}
	var entries []entry
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == root {
			if !d.IsDir() {
				return fmt.Errorf("%s is not a directory", dir)
			}
			return nil
		}
		if skipped(d.Name()) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		entries = append(entries, entry{filepath.ToSlash(rel), path, info.Mode()})
		return nil
	})
	if err != nil {
		return "", err
	}
	slices.SortFunc(entries, func(a, b entry) int { return strings.Compare(a.rel, b.rel) })

	h := sha256.New()
	for _, e := range entries {
		switch {
		case e.mode.IsDir():
			h.Write([]byte{'D'})
			writeField(h, []byte(e.rel))
		case e.mode.IsRegular():
			data, err := os.ReadFile(e.path)
			if err != nil {
				return "", err
			}
			var exec byte
			if e.mode&0o111 != 0 {
				exec = 1
			}
			h.Write([]byte{'F'})
			writeField(h, []byte(e.rel))
			h.Write([]byte{exec})
			writeField(h, data)
		case e.mode&fs.ModeSymlink != 0:
			target, err := os.Readlink(e.path)
			if err != nil {
				return "", err
			}
			h.Write([]byte{'L'})
			writeField(h, []byte(e.rel))
			writeField(h, []byte(target))
		default:
			return "", errors.New(e.rel + ": unsupported file type")
		}
	}
	return "v1:" + hex.EncodeToString(h.Sum(nil)), nil
}

func writeField(h hash.Hash, b []byte) {
	h.Write(binary.BigEndian.AppendUint64(nil, uint64(len(b))))
	h.Write(b)
}

// skipped reports whether an entry is left out of hashes and copies.
func skipped(name string) bool {
	return name == ".git" || name == ".DS_Store"
}
