package unnest

import (
	"strings"
	"testing"
)

const mondayAttribution = "On Mon 12 Oct 2026 22:27 +0000, Ada Example <ada@example.com> wrote:"

func TestNormaliseStandaloneMondayAttribution(t *testing.T) {
	for _, prefix := range []string{"", "> ", ">> "} {
		lines := Normalise(prefix + mondayAttribution)
		if len(lines) != 1 || lines[0].Text != mondayAttribution {
			t.Fatalf("normalised %q into %+v", prefix+mondayAttribution, lines)
		}
		b, ok := FindAttribution(lines, 0)
		if !ok || b.Text != mondayAttribution || b.Depth != lines[0].Depth {
			t.Fatalf("standalone attribution lost: %+v, found=%v", b, ok)
		}
	}
}

func TestInlineOpenerWordBoundaries(t *testing.T) {
	for _, prefix := range []string{"Mon", "carton", "éon", "αon", "字on", "e\u0301on", "1on", "_on", "CARTON", "replyON", "pale", "1On", "_On"} {
		body := prefix + " 12 Oct 2026 22:27 +0000, Ada Example <ada@example.com> wrote:"
		lines := Normalise(body)
		if len(lines) != 1 || lines[0].Text != body {
			t.Errorf("split an opener suffix inside a word: %q -> %+v", body, lines)
		}
	}
	for _, tc := range []struct{ prefix, opener, closer string }{
		{"Reply. ", "On", "wrote:"},
		{"Reply.", "On", "wrote:"},
		{"reply", "On", "wrote:"}, // HTML flattening can glue a title-case opener to a word.
		{"réponse", "Le", "a écrit :"},
		{"café", "On", "wrote:"},
		{"cafe\u0301", "On", "wrote:"},
		{"Reply. ", "Am", "schrieb:"},
		{"Reply. ", "W dniu", "napisał:"},
		{"Reply. ", "Vào", "đã viết:"},
	} {
		t.Run(tc.prefix+tc.opener, func(t *testing.T) {
			attr := tc.opener + " 12 Oct 2026 22:27 +0000, Ada Example <ada@example.com> " + tc.closer
			lines := Normalise(tc.prefix + attr + "Old message.")
			if len(lines) != 3 || lines[0].Text != strings.TrimRight(tc.prefix, " ") || lines[1].Text != attr || lines[2].Text != "Old message." {
				t.Fatalf("inline attribution lost: %+v", lines)
			}
			if _, ok := FindAttribution(lines, 1); !ok {
				t.Fatal("inline attribution not detected")
			}
		})
	}
}

func TestMondayAttributionWrappedAndInline(t *testing.T) {
	body := "Own reply. " + mondayAttribution + "Old message."
	blocks := Peel(body)
	if len(blocks) != 2 || blocks[0].Text != "Own reply." || blocks[1].Text != "Old message." || blocks[1].Sentinel != mondayAttribution {
		t.Fatalf("inline Monday attribution corrupted: %+v", blocks)
	}
	lines := Normalise(strings.Replace(mondayAttribution, " wrote:", "\n> wrote:", 1))
	b, ok := FindAttribution(lines, 0)
	if !ok || b.Text != mondayAttribution || b.End != 2 {
		t.Fatalf("wrapped Monday attribution lost: %+v, found=%v", b, ok)
	}
}
