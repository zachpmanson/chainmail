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
