package spec

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	// Registered for their side effect only: image.Decode sniffs the format, and
	// png is the one this file also encodes with.
	_ "image/gif"
	_ "image/jpeg"
)

// Previews turn an attachment into a picture the reader can see without leaving
// the page. Bytes are never in the corpus — it stores metadata only — so a
// preview exists exactly where the archive that fed the corpus also kept the
// file on disk. Today that is Slack, whose downloader writes every upload under
// __uploads/<file id>/<name>; mail attachments have no local bytes and stay
// chips until the mail CLI can fetch a part.
//
// The thumbnail is embedded in the page as a data: URI rather than linked. The
// page is one self-contained file whose whole point is that it reads with no
// network, and a linked thumbnail would make opening the page phone home for
// every image in it.

// previewMaxEdge is the long edge of an embedded thumbnail, in pixels. One copy
// of the bytes serves both the chip and the enlarged popover — the chip scales
// it down in CSS — so this is chosen to be legible at popover size rather than
// at chip size. Measured against real screenshots it lands around 50-80 KB of
// PNG each; going to 1024 roughly triples that for detail nobody reads off a
// transcript.
const previewMaxEdge = 640

// previewBudget caps the total encoded preview bytes on one page. A transcript
// with a hundred screenshots would otherwise produce a file too large to open,
// and the failure would be silent — the page would simply take a minute to
// load. Past the cap, attachments keep their chip and their link, which is the
// same treatment an attachment with no local bytes already gets.
const previewBudget = 4 << 20

// minPreviewEdge and minPreviewArea separate content from decoration.
//
// The measurement is on decoded pixels, not on the file's byte count. Byte size
// is only a proxy for "is this a real picture" and a bad one in both
// directions: a well-compressed 8 KB screenshot of an error dialog is content,
// and a 40 KB letterhead is not. Since a thumbnail requires decoding the image
// anyway, the dimensions are free and they measure the thing itself.
const (
	minPreviewEdge = 100
	minPreviewArea = 20_000
)

// inlinePartName matches the name a mail client gives a picture it embedded in
// the body rather than one a person chose to send: image001.png, image12.jpg.
// These are overwhelmingly signature logos and rules, they arrive in their
// hundreds, and no dimension test catches the larger ones. The name is the only
// signal that survives, so it is used on its own.
var inlinePartName = regexp.MustCompile(`(?i)^image\d{1,4}\.(png|jpe?g|gif)$`)

// previewer resolves an attachment's archived bytes and encodes thumbnails,
// spending at most previewBudget across the page.
type previewer struct {
	dir   string // archive upload root; empty disables previews entirely
	spent int
	// Skipped counts attachments that would have had a preview but lost it to
	// the budget, so the caller can say so rather than leave a silent hole.
	skipped int
}

// preview returns a data: URI for the attachment, or "" when it does not get
// one. Every "no" is a normal outcome: no local bytes, decoration, a format
// with no decoder, a corrupt file. None of them is worth failing a page over,
// because the chip is still correct without a picture.
func (p *previewer) preview(a attRow) (uri string, w, h int) {
	if p == nil || p.dir == "" || a.SourceRef == "" {
		return "", 0, 0
	}
	if !previewableMime(a.Mime) || inlinePartName.MatchString(a.Name) {
		return "", 0, 0
	}
	path, ok := p.locate(a)
	if !ok {
		return "", 0, 0
	}
	f, err := os.Open(path)
	if err != nil {
		return "", 0, 0
	}
	defer f.Close()
	src, _, err := image.Decode(f)
	if err != nil {
		return "", 0, 0
	}
	b := src.Bounds()
	if b.Dx() < minPreviewEdge || b.Dy() < minPreviewEdge ||
		b.Dx()*b.Dy() < minPreviewArea {
		return "", 0, 0
	}
	thumb := downsample(src, previewMaxEdge)
	var buf bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.BestCompression}
	if err := enc.Encode(&buf, thumb); err != nil {
		return "", 0, 0
	}
	// The budget is charged in encoded bytes before base64, which inflates by a
	// third; the cap is an order-of-magnitude guard, not an exact page size.
	if p.spent+buf.Len() > previewBudget {
		p.skipped++
		return "", 0, 0
	}
	p.spent += buf.Len()
	tb := thumb.Bounds()
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes()),
		tb.Dx(), tb.Dy()
}

// locate finds the archived file for an attachment. slackdump keys a directory
// by file id and puts the upload inside it under its own name, but the name in
// the corpus and the name on disk can disagree — the archive sanitises it — so
// the directory is authoritative and a lone file inside it is taken as the one.
func (p *previewer) locate(a attRow) (string, bool) {
	dir := filepath.Join(p.dir, a.SourceRef)
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
		if f == a.Name {
			return filepath.Join(dir, f), true
		}
	}
	if len(files) == 1 {
		return filepath.Join(dir, files[0]), true
	}
	return "", false
}

// previewableMime is the set of image types the standard library decodes. WebP
// and HEIC are deliberately absent: adding either means a dependency, and
// between them they account for a handful of attachments.
func previewableMime(mime string) bool {
	m := strings.ToLower(strings.TrimSpace(mime))
	if i := strings.Index(m, ";"); i >= 0 {
		m = strings.TrimSpace(m[:i])
	}
	switch m {
	case "image/png", "image/jpeg", "image/jpg", "image/gif":
		return true
	}
	return false
}

// downsample scales an image so its long edge is at most maxEdge, by averaging
// each destination pixel over the source box it covers.
//
// A box average rather than nearest-neighbour because these are screenshots of
// tables and text: dropping pixels turns thin rules into dashes and makes small
// type unreadable, which is exactly the content a preview is for. A box filter
// is not as sharp as Lanczos, but it needs no dependency and the difference is
// invisible at this size.
func downsample(src image.Image, maxEdge int) image.Image {
	b := src.Bounds()
	if b.Dx() <= maxEdge && b.Dy() <= maxEdge {
		return src
	}
	dw, dh := b.Dx(), b.Dy()
	if dw >= dh {
		dh = max(1, dh*maxEdge/dw)
		dw = maxEdge
	} else {
		dw = max(1, dw*maxEdge/dh)
		dh = maxEdge
	}
	dst := image.NewNRGBA(image.Rect(0, 0, dw, dh))
	for y := range dh {
		y0 := b.Min.Y + y*b.Dy()/dh
		y1 := max(y0+1, b.Min.Y+(y+1)*b.Dy()/dh)
		for x := range dw {
			x0 := b.Min.X + x*b.Dx()/dw
			x1 := max(x0+1, b.Min.X+(x+1)*b.Dx()/dw)
			var r, g, bl, al uint64
			for sy := y0; sy < y1; sy++ {
				for sx := x0; sx < x1; sx++ {
					cr, cg, cb, ca := src.At(sx, sy).RGBA()
					r += uint64(cr)
					g += uint64(cg)
					bl += uint64(cb)
					al += uint64(ca)
				}
			}
			n := uint64((y1 - y0) * (x1 - x0))
			i := dst.PixOffset(x, y)
			// RGBA() returns alpha-premultiplied 16-bit; NRGBA wants
			// non-premultiplied 8-bit, so undo the premultiplication before
			// narrowing or anything translucent darkens.
			a8 := uint8(al / n >> 8)
			dst.Pix[i+3] = a8
			if a8 == 0 {
				continue
			}
			dst.Pix[i+0] = unpremul(r/n, al/n)
			dst.Pix[i+1] = unpremul(g/n, al/n)
			dst.Pix[i+2] = unpremul(bl/n, al/n)
		}
	}
	return dst
}

func unpremul(c, a uint64) uint8 {
	if a == 0 {
		return 0
	}
	v := c * 0xffff / a >> 8
	if v > 0xff {
		v = 0xff
	}
	return uint8(v)
}
