package uploads

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
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

func TestOpenRejectsNonregularWithoutBlocking(t *testing.T) {
	if root := os.Getenv("CHAINMAIL_UPLOAD_SPECIAL_ROOT"); root != "" {
		var f *os.File
		var err error
		if os.Getenv("CHAINMAIL_UPLOAD_SPECIAL_STAGE") == "final" {
			r, openErr := os.OpenRoot(root)
			if openErr != nil {
				t.Fatal(openErr)
			}
			defer r.Close()
			// Exercise the final open directly: a regular candidate can be
			// replaced after discovery, before this open and fstat.
			f, err = openRegular(r, filepath.Join("F001", "marker.txt"))
		} else {
			f, err = Open(root, "F001", "marker.txt")
		}
		if f != nil {
			f.Close()
		}
		if err == nil || f != nil {
			t.Fatal("opened a nonregular file or escaped the root")
		}
		return
	}
	for _, tc := range []struct{ stage, kind string }{
		{"root", "fifo"},
		{"root", "file"},
		{"source", "fifo"},
		{"source", "symlink-fifo"},
		{"source", "file"},
		{"final", "fifo"},
		{"final", "symlink-fifo"},
		{"final", "directory"},
		{"final", "outside-symlink"},
	} {
		t.Run(tc.stage+"_"+tc.kind, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "uploads")
			target := root
			if tc.stage == "source" {
				target = filepath.Join(root, "F001")
			} else if tc.stage == "final" {
				target = filepath.Join(root, "F001", "marker.txt")
			}
			if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
				t.Fatal(err)
			}
			var err error
			switch tc.kind {
			case "fifo":
				err = syscall.Mkfifo(target, 0600)
			case "symlink-fifo":
				if err = syscall.Mkfifo(target+".fifo", 0600); err == nil {
					err = os.Symlink(filepath.Base(target)+".fifo", target)
				}
			case "file":
				err = os.WriteFile(target, []byte("inert marker"), 0600)
			case "directory":
				err = os.Mkdir(target, 0700)
			case "outside-symlink":
				outside := write(t, t.TempDir(), "private", "marker.txt")
				err = os.Symlink(outside, target)
			}
			if err != nil {
				t.Fatal(err)
			}
			// Kill a blocked open rather than leaving a goroutine and fd behind.
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestOpenRejectsNonregularWithoutBlocking$")
			cmd.Env = append(os.Environ(), "CHAINMAIL_UPLOAD_SPECIAL_ROOT="+root, "CHAINMAIL_UPLOAD_SPECIAL_STAGE="+tc.stage)
			out, err := cmd.CombinedOutput()
			if ctx.Err() != nil {
				t.Fatalf("open blocked: %v\n%s", ctx.Err(), out)
			}
			if err != nil {
				t.Fatalf("subprocess: %v\n%s", err, out)
			}
		})
	}
}

func TestOpenKeepsInternalSymlinksAndTheOpenedHandle(t *testing.T) {
	root := t.TempDir()
	path := write(t, root, "F001", "marker.txt")
	if err := os.Symlink("F001", filepath.Join(root, "linked")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("marker.txt", filepath.Join(root, "F001", "linked.txt")); err != nil {
		t.Fatal(err)
	}
	f, err := Open(root, "linked", "linked.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := os.Rename(path, path+".old"); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(path, 0600); err != nil {
		t.Fatal(err)
	}
	b, err := io.ReadAll(f)
	if err != nil || string(b) != "bytes" {
		t.Fatalf("retained handle: %q, %v", b, err)
	}
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
