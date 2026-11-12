package spec

import (
	"strings"
	"testing"

	"github.com/zachpmanson/chainmail/internal/corpus"
)

func TestRestoredReplyKeepsOneQuoteInBothRenderings(t *testing.T) {
	for _, body := range []string{"A date.\n\n> an earlier quote\n", ""} {
		for _, own := range []string{"**Agreed.**", "Agreed.\n", "Agreed.\n\n\n"} {
			target := corpus.ReplyTarget{
				Author: "Bo Halvorsen", From: "bo@fjordline.example", TS: at(t),
				Body: body, HTML: "<p>A <b>date</b>.</p>",
			}
			first := ComposeReply(own, target)
			restored := first.Text
			for i := 0; i < 3; i++ {
				got := ComposeReply(restored, target)
				if got != first {
					t.Fatalf("round %d changed reply (own=%q, parent=%q):\ngot %+v\nwant %+v", i, own, body, got, first)
				}
				restored = got.Text
			}
			if strings.Count(first.HTML, `<blockquote class="gmail_quote">`) != 1 {
				t.Fatalf("missing HTML quote: %s", first.HTML)
			}
		}
	}
}

func TestReplyDeduplicationDoesNotDiscardAuthoredQuotes(t *testing.T) {
	target := corpus.ReplyTarget{Author: "Bo Halvorsen", Body: "The date.", TS: at(t)}
	quote := ComposeReply("", target).Text
	for _, own := range []string{
		"> My own quotation.\n",
		strings.ReplaceAll(quote, "The date.", "Another date."),
		quote + "> An extra quoted line.\n",
		"not a paragraph boundary" + strings.TrimPrefix(quote, "\n\n"),
	} {
		got := ComposeReply(own, target)
		if !strings.HasPrefix(got.Text, own) || !strings.HasSuffix(got.Text, quote) {
			t.Fatalf("authored text was lost: own=%q, reply=%q", own, got.Text)
		}
	}
}

func TestWordsAddedBelowRestoredQuoteStayBelowIt(t *testing.T) {
	target := corpus.ReplyTarget{Author: "Bo Halvorsen", Body: "The date.", HTML: "<p>The <b>date</b>.</p>", TS: at(t)}
	first := ComposeReply("My answer.", target)
	body := first.Text + "\n**One more thought.**"
	got := ComposeReply(body, target)
	if got.Text != body || strings.Count(got.HTML, `<blockquote class="gmail_quote">`) != 1 {
		t.Fatalf("restored quote duplicated: %+v", got)
	}
	if !strings.Contains(got.HTML, "<b>date</b>") || !strings.Contains(got.HTML, "</blockquote>\n<p><strong>One more thought.</strong></p>") {
		t.Fatalf("quote markup or words below it lost: %s", got.HTML)
	}
	if again := ComposeReply(got.Text, target); again != got {
		t.Fatalf("resave changed reply: %+v, want %+v", again, got)
	}
}
