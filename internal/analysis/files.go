package analysis

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

const (
	// Most of a file sent to the model, longer files are cut off
	maxFileBytes = 32 * 1024
	// Most of the README sent along with the overview
	maxReadmeBytes = 16 * 1024
	// How much of a file is checked for NUL bytes to spot binary files
	binarySniffBytes = 8 * 1024
)

// Reasons a file isn't sent to the model
const (
	SkipBinary     = "binary"
	SkipEmpty      = "empty"
	SkipLockfile   = "lockfile"
	SkipMinified   = "minified"
	SkipSymlink    = "symlink"
	SkipUnreadable = "unreadable"
)

// Used as the summary of files that aren't sent to the model
var SKIPPED_PURPOSES = map[string]string{
	SkipBinary:     "Binary file.",
	SkipEmpty:      "Empty file.",
	SkipLockfile:   "Lockfile pinning dependency versions.",
	SkipMinified:   "Minified or generated build output.",
	SkipSymlink:    "Symbolic link.",
	SkipUnreadable: "File that couldn't be read.",
}

// Files that only pin dependency versions, which are long and say little
var LOCKFILES = map[string]struct{}{
	"package-lock.json":   {},
	"npm-shrinkwrap.json": {},
	"yarn.lock":           {},
	"pnpm-lock.yaml":      {},
	"bun.lock":            {},
	"go.sum":              {},
	"Cargo.lock":          {},
	"poetry.lock":         {},
	"Pipfile.lock":        {},
	"uv.lock":             {},
	"pdm.lock":            {},
	"Gemfile.lock":        {},
	"composer.lock":       {},
	"mix.lock":            {},
	"pubspec.lock":        {},
	"Podfile.lock":        {},
	"flake.lock":          {},
	"packages.lock.json":  {},
}

var (
	errNotRegularFile = errors.New("not a regular file")
	errNotDirectory   = errors.New("not a directory")
)

// what inspecting a file finds out
type fileInfo struct {
	hash string
	size int64
	// why the file won't be sent to the model, empty if it will
	skipped string
}

// Hash a file and decide whether it should be summarized
func inspectFile(abs_path string) fileInfo {
	info, err := os.Lstat(abs_path)
	if err != nil {
		return fileInfo{skipped: SkipUnreadable}
	}
	// never followed, a symlink in an untrusted repo could point anywhere,
	// like a private key that would then be sent to the model
	if info.Mode()&fs.ModeSymlink != 0 {
		target, _ := os.Readlink(abs_path)
		return fileInfo{hash: hashString("symlink " + target), skipped: SkipSymlink}
	}

	file, err := openRegular(abs_path)
	if err != nil {
		return fileInfo{skipped: SkipUnreadable}
	}
	defer file.Close()

	hash := sha256.New()
	head := make([]byte, binarySniffBytes)
	head_size, err := io.ReadFull(file, head)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return fileInfo{skipped: SkipUnreadable}
	}
	head = head[:head_size]
	hash.Write(head)
	rest_size, err := io.Copy(hash, file)
	if err != nil {
		return fileInfo{skipped: SkipUnreadable}
	}

	result := fileInfo{
		hash: hex.EncodeToString(hash.Sum(nil)),
		size: int64(head_size) + rest_size,
	}
	name := filepath.Base(abs_path)
	switch {
	case result.size == 0:
		result.skipped = SkipEmpty
	case bytes.IndexByte(head, 0) >= 0:
		result.skipped = SkipBinary
	case isLockfile(name):
		result.skipped = SkipLockfile
	case isMinified(name):
		result.skipped = SkipMinified
	}
	return result
}

func isLockfile(name string) bool {
	_, ok := LOCKFILES[name]
	return ok
}

func isMinified(name string) bool {
	return strings.HasSuffix(name, ".min.js") || strings.HasSuffix(name, ".min.css") || strings.HasSuffix(name, ".map")
}

// Open a file only if it is a regular file, so symlinks aren't followed
// and opening something like a named pipe can't block forever
func openRegular(abs_path string) (*os.File, error) {
	info, err := os.Lstat(abs_path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, &fs.PathError{Op: "open", Path: abs_path, Err: errNotRegularFile}
	}
	return os.Open(abs_path)
}

var ErrOutsideRepo = errors.New("path is outside the repo")

// Read up to limit bytes from the start of a file in a repo. rel_path uses
// forward slashes and must stay inside the repo, and no symlink along the
// way is followed, so an untrusted repo can't get a file outside it read.
func ReadRepoFile(repo_path, rel_path string, limit int64) ([]byte, error) {
	clean := path.Clean(rel_path)
	if path.IsAbs(clean) || clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return nil, fmt.Errorf("%w: %s", ErrOutsideRepo, rel_path)
	}

	// Lstat only looks at the last part of a path, so the directories
	// leading to the file are checked one at a time
	parts := strings.Split(clean, "/")
	current := repo_path
	for _, part := range parts[:len(parts)-1] {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			return nil, &fs.PathError{Op: "open", Path: current, Err: errNotDirectory}
		}
	}
	return readHead(filepath.Join(current, parts[len(parts)-1]), limit)
}

// Read up to limit bytes from the start of a regular file
func readHead(abs_path string, limit int64) ([]byte, error) {
	file, err := openRegular(abs_path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return io.ReadAll(io.LimitReader(file, limit))
}

// Hash a directory from its children's names and hashes, so it changes
// whenever anything under it does
func hashDir(dir *dirEntry) string {
	hash := sha256.New()
	for _, child := range dir.dirs {
		fmt.Fprintf(hash, "dir %q %s\n", path.Base(child.rel_path), child.hash)
	}
	for _, file := range dir.files {
		fmt.Fprintf(hash, "file %q %s\n", path.Base(file.rel_path), file.hash)
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func hashString(s string) string {
	hash := sha256.Sum256([]byte(s))
	return hex.EncodeToString(hash[:])
}
