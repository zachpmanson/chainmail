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

func TestSplitDraftReplyPreservesWordsAndDeclinesAmbiguousQuotes(t *testing.T) {
	target := corpus.ReplyTarget{Author: "Bo", Body: "A date.\n\n> Earlier", TS: at(t)}
	quote := ComposeReply("", target).Text
	for _, before := range []string{"", "My words.\n\n> My quotation.\n"} {
		for _, after := range []string{"", "\nWords below.\n\n> My other quotation.\n"} {
			body := before + quote + after
			split := SplitDraftReply(body, target)
			if split == nil || split.Before != before || split.After != after || split.Before+"\n\n"+split.Quote+split.After != body {
				t.Fatalf("split changed %q: %+v", body, split)
			}
			if got := ComposeReply("Edited: "+split.Before+"\n\n"+split.Quote+split.After, target).Text; got != "Edited: "+body {
				t.Fatalf("edited round trip = %q; wanted %q", got, "Edited: "+body)
			}
		}
	}
	for _, body := range []string{
		"Only my own words.",
		strings.ReplaceAll(quote, "A date.", "Another date."),
		quote + "  > An extra quoted line.\n",
		quote + quote,
		quote + quote[1:],
	} {
		if got := SplitDraftReply(body, target); got != nil {
			t.Fatalf("hid edited or ambiguous text in %q: %+v", body, got)
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
