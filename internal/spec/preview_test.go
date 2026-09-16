package spec

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Every picture here is drawn in the test. Nothing under testdata, nothing
// committed: the corpus this reads in anger holds real screenshots of real
// customers, and the surest way never to leak one is to have none in the repo.

// draw writes a w×h PNG into the archive layout the Slack downloader uses —
// one directory per file id, the upload inside it under its own name.
func draw(t *testing.T, root, id, name string, w, h int) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			// A gradient rather than a flat fill: a flat image compresses to
			// almost nothing, which would make the budget test meaningless.
			img.Set(x, y, color.RGBA{uint8(x * 7), uint8(y * 5), uint8(x ^ y), 0xff})
		}
	}
	dir := filepath.Join(root, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}

func TestPreviewIsGivenToContentAndWithheldFromDecoration(t *testing.T) {
	root := t.TempDir()
	draw(t, root, "F001", "screenshot.png", 1200, 800)
	draw(t, root, "F002", "logo.png", 60, 60)        // too small either way
	draw(t, root, "F003", "image001.png", 1200, 800) // inline part, content-sized
	draw(t, root, "F004", "rule.png", 900, 40)       // a divider: one edge too short
	draw(t, root, "F005", "badge.png", 120, 120)     // clears the edge floor, not the area

	cases := []struct {
		ref, name, mime string
		want            bool
		why             string
	}{
		{"F001", "screenshot.png", "image/png", true, "a content-sized screenshot"},
		{"F002", "logo.png", "image/png", false, "a signature logo"},
		{"F003", "image001.png", "image/png", false, "an inline part, whatever its size"},
		{"F004", "rule.png", "image/png", false, "a letterhead rule"},
		{"F005", "badge.png", "image/png", false, "an icon that clears the edge floor"},
		{"F001", "screenshot.png", "application/pdf", false, "a mime with no decoder"},
		{"F404", "gone.png", "image/png", false, "an attachment whose bytes were never archived"},
		{"", "part.png", "image/png", false, "a mail part, which has no archived bytes at all"},
	}
	for _, c := range cases {
		p := &previewer{dir: root}
		uri, w, h := p.preview(attRow{Name: c.name, Mime: c.mime, SourceRef: c.ref})
		if got := uri != ""; got != c.want {
			t.Errorf("%s: preview = %v, want %v", c.why, got, c.want)
		}
		if c.want && (w == 0 || h == 0) {
			t.Errorf("%s: preview came back without dimensions (%d×%d)", c.why, w, h)
		}
		if !c.want && (w != 0 || h != 0) {
			t.Errorf("%s: no preview but dimensions %d×%d", c.why, w, h)
		}
	}
}

func TestPreviewIsEmbeddedNotLinked(t *testing.T) {
	root := t.TempDir()
	draw(t, root, "F001", "screenshot.png", 1200, 800)
	p := &previewer{dir: root}
	uri, w, h := p.preview(attRow{Name: "screenshot.png", Mime: "image/png", SourceRef: "F001"})

	if !strings.HasPrefix(uri, "data:image/png;base64,") {
		t.Fatalf("preview is not an embedded data URI: %.40q", uri)
	}
	// The page's whole claim is that it reads with no network. A preview that
	// pointed anywhere would break that on load, for every image at once.
	if strings.Contains(uri, "http") {
		t.Errorf("preview URI references a host: %.60q", uri)
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(uri, "data:image/png;base64,"))
	if err != nil {
		t.Fatalf("preview payload is not base64: %v", err)
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("preview payload is not a PNG: %v", err)
	}
	if cfg.Width != w || cfg.Height != h {
		t.Errorf("declared %d×%d but encoded %d×%d", w, h, cfg.Width, cfg.Height)
	}
	if cfg.Width != previewMaxEdge {
		t.Errorf("long edge = %d, want %d", cfg.Width, previewMaxEdge)
	}
	// 1200×800 scaled to a 640 long edge. The height follows the ratio, so a
	// thumbnail is never stretched.
	if want := 800 * previewMaxEdge / 1200; cfg.Height != want {
		t.Errorf("height = %d, want %d — aspect ratio was not preserved", cfg.Height, want)
	}
}

func TestPreviewsStopAtTheBudgetAndSayHowMany(t *testing.T) {
	root := t.TempDir()
	for _, id := range []string{"F001", "F002", "F003"} {
		draw(t, root, id, "shot.png", 1200, 800)
	}
	// A budget that admits the first thumbnail and nothing after it.
	p := &previewer{dir: root}
	first, _, _ := p.preview(attRow{Name: "shot.png", Mime: "image/png", SourceRef: "F001"})
	if first == "" {
		t.Fatal("the first preview should fit in an empty budget")
	}
	p.spent = previewBudget
	for _, id := range []string{"F002", "F003"} {
		if uri, _, _ := p.preview(attRow{Name: "shot.png", Mime: "image/png", SourceRef: id}); uri != "" {
			t.Errorf("%s got a preview past the budget", id)
		}
	}
	if p.skipped != 2 {
		t.Errorf("skipped = %d, want 2 — a page that dropped previews must be able to say so", p.skipped)
	}
}

func TestBytesAreFoundWhenTheArchiveRenamedTheFile(t *testing.T) {
	root := t.TempDir()
	// The downloader sanitises a name it cannot write, so the name in the corpus
	// and the name on disk disagree. The file id is the reliable half.
	draw(t, root, "F001", "screen_shot.png", 1200, 800)
	p := &previewer{dir: root}
	if uri, _, _ := p.preview(attRow{Name: "Screen Shot 1/2.png", Mime: "image/png", SourceRef: "F001"}); uri == "" {
		t.Error("a lone file in the id's directory should be taken as the attachment")
	}

	// With two candidates and no name match there is no honest choice, so the
	// attachment keeps its chip rather than showing the wrong picture.
	draw(t, root, "F002", "one.png", 1200, 800)
	draw(t, root, "F002", "two.png", 1200, 800)
	if uri, _, _ := p.preview(attRow{Name: "neither.png", Mime: "image/png", SourceRef: "F002"}); uri != "" {
		t.Error("an ambiguous directory should not guess which file is the attachment")
	}
}

func TestNoUploadDirMeansNoPreviews(t *testing.T) {
	// The zero previewer is what a caller that never opted in gets, and it must
	// not touch the filesystem or fail.
	p := &previewer{}
	if uri, _, _ := p.preview(attRow{Name: "shot.png", Mime: "image/png", SourceRef: "F001"}); uri != "" {
		t.Error("previews were produced without an upload directory")
	}
}

func TestDownsampleLeavesASmallImageAlone(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 200, 120))
	if got := downsample(src, previewMaxEdge); got != image.Image(src) {
		t.Error("an image already under the long edge was re-encoded for no reason")
	}
	tall := image.NewRGBA(image.Rect(0, 0, 300, 1500))
	b := downsample(tall, previewMaxEdge).Bounds()
	if b.Dy() != previewMaxEdge {
		t.Errorf("tall image: height = %d, want %d — the long edge is the one capped", b.Dy(), previewMaxEdge)
	}
	if b.Dx() != 300*previewMaxEdge/1500 {
		t.Errorf("tall image: width = %d, want %d", b.Dx(), 300*previewMaxEdge/1500)
	}
}

// The point of the corpus-first order: a page built on a host with no archive
// still shows what was pulled, which is what makes a deliberate pull worth doing.
func TestAPulledAttachmentPreviewsWithNoArchiveAtAll(t *testing.T) {
	data := encodePNG(t, 1200, 800)
	p := &previewer{blob: func(sha string) ([]byte, bool) {
		if sha != "abc123" {
			return nil, false
		}
		return data, true
	}}
	uri, w, h := p.preview(attRow{Name: "shot.png", Mime: "image/png", SourceRef: "part-1", BlobSHA: "abc123"})
	if uri == "" {
		t.Fatal("a pulled attachment produced no preview, even from the corpus")
	}
	// Thumbnailed to the long edge, which is itself the proof it came from the
	// 1200-wide copy: an archive file under the edge would come back at its own size.
	if w != previewMaxEdge || h == 0 {
		t.Errorf("preview is %d×%d, want a %d-wide thumbnail", w, h, previewMaxEdge)
	}

	// A link whose blob has been pruned falls back to the archive rather than
	// leaving a hole, and neither source is an error.
	root := t.TempDir()
	draw(t, root, "part-1", "shot.png", 300, 200)
	stale := &previewer{dir: root, blob: func(string) ([]byte, bool) { return nil, false }}
	if uri, _, _ := stale.preview(
		attRow{Name: "shot.png", Mime: "image/png", SourceRef: "part-1", BlobSHA: "pruned"}); uri == "" {
		t.Error("a stale blob link discarded the archive copy")
	}
}

func TestTheCorpusIsAskedBeforeTheArchive(t *testing.T) {
	// Deliberately different shapes, so which source answered is visible in the
	// result rather than inferred from a call count.
	root := t.TempDir()
	draw(t, root, "F001", "shot.png", 300, 200)
	pulled := encodePNG(t, 1200, 800)

	asked := 0
	p := &previewer{dir: root, blob: func(sha string) ([]byte, bool) {
		asked++
		return pulled, sha == "abc123"
	}}
	_, w, h := p.preview(attRow{Name: "shot.png", Mime: "image/png", SourceRef: "F001", BlobSHA: "abc123"})
	if w != previewMaxEdge {
		t.Fatalf("preview is %d×%d, want a %d-wide thumbnail of the pulled copy", w, h, previewMaxEdge)
	}
	if asked == 0 {
		t.Error("the corpus was never consulted")
	}

	// An attachment that was never pulled must not consult the corpus at all:
	// the digest is the only thing it would have to look up.
	asked = 0
	p2 := &previewer{dir: root, blob: p.blob}
	if _, w, h := p2.preview(attRow{Name: "shot.png", Mime: "image/png", SourceRef: "F001"}); w != 300 {
		t.Fatalf("archive-only preview is %d×%d, want 300×200", w, h)
	}
	if asked != 0 {
		t.Errorf("looked up an attachment with no digest %d times", asked)
	}
}

// encodePNG draws a gradient PNG in memory, for tests that never touch disk.
func encodePNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{uint8(x * 7), uint8(y * 5), uint8(x ^ y), 0xff})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
