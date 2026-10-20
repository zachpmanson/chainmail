// Package uploads finds an archived attachment's bytes on disk.
//
// Slack's downloader writes uploads under directories named by file id. The
// archive may sanitise filenames, so a lone file is used when its name differs.
package uploads

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// Open opens a regular archived file without allowing references or symlinks to
// escape the configured upload root. The caller owns the returned file.
func Open(root, sourceRef, name string) (*os.File, error) {
	if root == "" || sourceRef == "" || sourceRef == "." || sourceRef == ".." || strings.ContainsAny(sourceRef, `/\\`) || filepath.IsAbs(sourceRef) {
		return nil, os.ErrNotExist
	}
	// A trailing separator requires a directory before OpenRoot's fstat.
	r, err := os.OpenRoot(root + string(os.PathSeparator))
	if err != nil {
		return nil, err
	}
	defer r.Close()
	// OpenRoot also opens before checking the type; require a directory here.
	dir, err := r.OpenFile(sourceRef, os.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	ents, err := dir.ReadDir(-1)
	dir.Close()
	if err != nil {
		return nil, err
	}
	var files []string
	for _, e := range ents {
		info, err := r.Stat(filepath.Join(sourceRef, e.Name()))
		if err == nil && info.Mode().IsRegular() {
			files = append(files, e.Name())
		}
	}
	chosen := ""
	for _, f := range files {
		if f == name {
			chosen = f
			break
		}
	}
	if chosen == "" && len(files) == 1 {
		chosen = files[0]
	}
	if chosen == "" {
		return nil, os.ErrNotExist
	}
	return openRegular(r, filepath.Join(sourceRef, chosen))
}

func openRegular(r *os.Root, name string) (*os.File, error) {
	// The candidate can become a FIFO after Stat; never wait for a writer.
	file, err := r.OpenFile(name, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		file.Close()
		return nil, os.ErrNotExist
	}
	return file, nil
}

// Locate returns the archived path for callers that need a filename. A second
// candidate without an exact name match is ambiguous and is never guessed.
func Locate(root, sourceRef, name string) (string, bool) {
	f, err := Open(root, sourceRef, name)
	if err != nil {
		return "", false
	}
	defer f.Close()
	return f.Name(), true
}
