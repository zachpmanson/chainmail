// Package uploads finds an archived attachment's bytes on disk.
//
// Slack's downloader writes every upload under a directory named by the file id,
// and two callers need to walk that: the renderer, to thumbnail a picture it does
// not hold in the corpus, and the media pull, to import the bytes so the renderer
// stops needing a directory at all. The walk lives here rather than in either of
// them, because two implementations of "which file is this attachment" drift in
// exactly the case that matters — a name the archive sanitised on the way in.
package uploads

import (
	"os"
	"path/filepath"
)

// Locate returns the archived file for one attachment, given the upload root and
// the source's own handle on it.
//
// slackdump keys a directory by file id and puts the upload inside it under its
// own name, but the name in the corpus and the name on disk can disagree — the
// archive sanitises it — so the directory is authoritative and a lone file inside
// it is taken as the one. A second file leaves the answer ambiguous, and a wrong
// guess would attach the wrong picture to a message, so it returns false.
func Locate(root, sourceRef, name string) (string, bool) {
	if root == "" || sourceRef == "" {
		return "", false
	}
	dir := filepath.Join(root, sourceRef)
	ents, err := os.ReadDir(dir)
	if err != nil {
		return "", false
	}
	var files []string
	for _, e := range ents {
		if !e.IsDir() {
			files = append(files, e.Name())
		}
	}
	if len(files) == 0 {
		return "", false
	}
	for _, f := range files {
		if f == name {
			return filepath.Join(dir, f), true
		}
	}
	if len(files) == 1 {
		return filepath.Join(dir, files[0]), true
	}
	return "", false
}
