package uploads

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, root, id, name string) string {
	t.Helper()
	dir := filepath.Join(root, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLocateFindsTheFileByItsName(t *testing.T) {
	root := t.TempDir()
	want := write(t, root, "F001", "shot.png")
	got, ok := Locate(root, "F001", "shot.png")
	if !ok || got != want {
		t.Fatalf("Locate: got %q ok=%v, want %q", got, ok, want)
	}
}

// The downloader sanitises a name it cannot write, so the name in the corpus and
// the name on disk disagree. The file id is the reliable half.
func TestLocateTakesTheOnlyFileInTheDirectory(t *testing.T) {
	root := t.TempDir()
	write(t, root, "F001", "shot_1_2.png")
	if _, ok := Locate(root, "F001", "shot 1/2.png"); !ok {
		t.Error("a lone file in the id's directory should be taken as the attachment")
	}
}

// Two candidates and no name match: there is no honest choice, and a wrong guess
// attaches the wrong picture to a message.
func TestLocateRefusesToGuessBetweenTwoFiles(t *testing.T) {
	root := t.TempDir()
	write(t, root, "F001", "one.png")
	write(t, root, "F001", "two.png")
	if got, ok := Locate(root, "F001", "neither.png"); ok {
		t.Errorf("guessed %q out of an ambiguous directory", got)
	}
}

func TestLocateWithoutARootOrAReference(t *testing.T) {
	root := t.TempDir()
	write(t, root, "F001", "shot.png")
	if _, ok := Locate("", "F001", "shot.png"); ok {
		t.Error("located a file with no archive root")
	}
	if _, ok := Locate(root, "", "shot.png"); ok {
		t.Error("located a file with no reference to locate it by")
	}
	if _, ok := Locate(root, "F404", "shot.png"); ok {
		t.Error("located a file that is not there")
	}
}

func TestLocateRejectsEscapesAndNonFiles(t *testing.T) {
	root := t.TempDir()
	uploads := filepath.Join(root, "uploads")
	if err := os.Mkdir(uploads, 0700); err != nil {
		t.Fatal(err)
	}
	outside := write(t, root, "private", "marker.txt")
	write(t, uploads, "F001", "inside.txt")
	if err := os.Symlink(filepath.Dir(outside), filepath.Join(uploads, "linked")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(uploads, "F001", "linked.txt")); err != nil {
		t.Fatal(err)
	}
	for _, ref := range []string{"../private", filepath.Dir(outside), ".", "F001/../../private", `..\private`, "linked"} {
		if path, ok := Locate(uploads, ref, "marker.txt"); ok {
			t.Errorf("escaped with %q: %q", ref, path)
		}
	}
	if path, ok := Locate(uploads, "F001", "linked.txt"); ok && path != filepath.Join(uploads, "F001", "inside.txt") {
		t.Errorf("followed external file link: %q", path)
	}
	if err := os.Mkdir(filepath.Join(uploads, "empty"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(uploads, "empty", "marker.txt")); err != nil {
		t.Fatal(err)
	}
	if path, ok := Locate(uploads, "empty", "marker.txt"); ok {
		t.Errorf("followed sole external link: %q", path)
	}
}
