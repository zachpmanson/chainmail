package unnest

import (
	"strings"
	"testing"
)

func TestEveryHeaderAliasThroughPipeline(t *testing.T) {
	groups := []struct{ field, aliases, value string }{
		{"from", "from von de da van fra", "Alex <alex@example.test>"},
		{"replyto", "reply-to", "Other <other@example.test>"},
		{"date", "sent date gesendet enviado envoyé verzonden inviato sendt skickat", "5 Oct 2026 22:07"},
		{"to", "to an para aan till a", "Robin <robin@example.test>"},
		{"subject", "subject betreff asunto objet oggetto ämne assunto onderwerp", "Bulletin"},
		{"cc", "cc bcc", "Casey <casey@example.test>"},
		{"messageid", "message-id", "<synthetic@example.test>"},
	}
	tested := 0
	for _, group := range groups {
		for _, alias := range strings.Fields(group.aliases) {
			tested++
			for _, shape := range []string{alias + ": ", "*" + strings.ToUpper(alias) + ":* ", "*" + alias + "*: ", "*" + alias + ": * "} {
				t.Run(shape, func(t *testing.T) {
					line := shape + group.value
					field, value, ok := parseHeaderLine(line)
					if !ok || field != group.field || value != group.value {
						t.Fatalf("lookup: %s %q %v", field, value, ok)
					}
					blocks := Peel("> From: Alex <alex@example.test>\n> " + line + "\n\n> Synthetic bulletin.")
					if len(blocks) != 1 || blocks[0].Kind != KindHeaderBlock || blocks[0].Depth != 1 {
						t.Fatalf("boundary disagrees: %+v", blocks)
					}
					r := Parse(blocks[0])
					if r.Block.Text != "Synthetic bulletin." || r.Sender != "Alex" || r.Address != "alex@example.test" {
						t.Fatalf("metadata/body changed: %+v", r)
					}
					switch group.field {
					case "date":
						if r.Sent.Format("2006-01-02 15:04") != "2026-10-05 22:07" {
							t.Errorf("lost date: %+v", r)
						}
					case "to":
						if r.To != group.value {
							t.Errorf("lost To: %q", r.To)
						}
					case "cc":
						if r.Cc != group.value {
							t.Errorf("lost Cc: %q", r.Cc)
						}
					case "subject":
						if r.Subject != group.value {
							t.Errorf("lost subject: %q", r.Subject)
						}
					case "messageid":
						if r.MessageID != "synthetic@example.test" || r.Key != "mail:synthetic@example.test" {
							t.Errorf("lost Message-ID: %+v", r)
						}
					}
				})
			}
		}
	}
	if tested != len(headerKeyAlias) {
		t.Errorf("alias vocabulary changed without a fixture: tested=%d aliases=%d", tested, len(headerKeyAlias))
	}
}

func TestAccentedLabelsAndSourceNames(t *testing.T) {
	// Only structural/date tokens are NFC-normalised, not the author's display name.
	name := "Jose\u0301 Vale"
	body := "De: " + name + " <alex@example.test>\n*Envoye\u0301:* lun. 5 oct. 2026 à 22:07\n*Objet:* Bulletin\n\nSynthetic bulletin."
	blocks := Peel(body)
	if len(blocks) != 1 {
		t.Fatalf("blocks=%d", len(blocks))
	}
	r := Parse(blocks[0])
	if r.Sender != name || r.DateReason != DateParsed || r.Subject != "Bulletin" {
		t.Errorf("accented header not retained: %+v", r)
	}
	a := ParseAttribution("Le lun. 5 oct. 2026 à 22:07, " + name + " <alex@example.test> a écrit :")
	if a.Sender != name || a.DateReason != DateParsed {
		t.Errorf("author name rewritten: %+v", a)
	}
}

func TestHeaderAndAttributionSafetyNegatives(t *testing.T) {
	for _, body := range []string{
		"A: this is prose\nCc: this is emphasis\n\nNo quoted message.",
		"*To:* Write a summary\n*CC:* Ask for review\n\nNo quoted message.",
		"Reply-To: Alex <alex@example.test>\nTo: Robin <robin@example.test>\n\nNo sender stated.",
		"From:\nTo: Robin <robin@example.test>\n\nNo sender stated.",
		"From: Alex <alex@example.test>\nSubject:\n\nNot two stated header values.",
		"Meeting ID: 1234\nPasscode: 5678\n\nNo quoted message.",
		"Not-A-Header: Alex <alex@example.test>\nUnrecognised: 5 Oct 2026 22:07\n\nNo quoted message.",
		"This is prose On 03/04/2026 22:07, Alex <alex@example.test> wrote: still prose.",
		"This is prose On 5 Oct 2026 25:61, Alex <alex@example.test> wrote: still prose.",
		"On 5 Oct 2026 22:07, Alex <alex@example.test>\n\nwrote:",
	} {
		blocks := Peel(body)
		if len(blocks) != 1 || blocks[0].Sentinel != "" {
			t.Errorf("prose became a phantom quoted item: %+v", blocks)
		}
	}
}

func TestFoldedLocalizedValues(t *testing.T) {
	for _, body := range []string{
		"Da: Alex Vale\n<alex@example.test>\nDate: lun 5 ott 2026,\n22:07\nA: Robin <robin@example.test>,\nCasey <casey@example.test>\nOggetto: Bulletin\n\nSynthetic bulletin.",
		"Da: Alex Vale <alex@example.test>\nA: Robin <robin@example.test>\nDate: lun 5 ott 2026,\n22:07\n\nSynthetic bulletin.",
	} {
		blocks := Peel(body)
		if len(blocks) != 1 {
			t.Fatalf("blocks=%d", len(blocks))
		}
		r := Parse(blocks[0])
		if r.Address != "alex@example.test" || r.DateReason != DateParsed || r.Sent.Format("2006-01-02 15:04") != "2026-10-05 22:07" || r.Block.Text != "Synthetic bulletin." || !strings.Contains(r.To, "robin@example.test") {
			t.Errorf("folded metadata not recovered: %+v", r)
		}
	}
}

func TestConflictingDatesAreUnknown(t *testing.T) {
	for _, second := range []string{"6 Oct 2026 22:07", "5 Oct 2026 22:08", "5 Oct 2026 22:07 +0200", "03/04/2026 22:07", "5 Oct 2026 25:61"} {
		blocks := Peel("From: Alex <alex@example.test>\nDate: 5 Oct 2026 22:07\nSent: " + second + "\n\nSynthetic bulletin.")
		r := Parse(blocks[0])
		if !r.Sent.IsZero() || r.DateReason != DateContradictory || r.DatePrecision != PrecisionUnknown || r.Address != "alex@example.test" {
			t.Errorf("conflict became stated metadata: %+v", r)
		}
	}
	r := ParseHeaderBlock("From: Alex <alex@example.test>\nDate: lun 5 ott 2026 22:07\nSent: 5 October 2026 22:07:00")
	if r.DateReason != DateParsed || r.DatePrecision != PrecisionSecond {
		t.Errorf("equivalent repeated dates lost evidence: %+v", r)
	}
}

func TestForwardRuleFollowedByAttribution(t *testing.T) {
	for _, rule := range []string{"---------- Forwarded message ---------", "____________"} {
		blocks := Peel(rule + "\nIl lun 5 ott 2026 alle 22:07, Alex <alex@example.test> ha scritto:\n\nSynthetic bulletin.")
		if len(blocks) != 1 || blocks[0].Kind != KindForwardRule {
			t.Fatalf("forward boundary changed: %+v", blocks)
		}
		r := Parse(blocks[0])
		if r.DateReason != DateParsed || r.Address != "alex@example.test" || r.Subject != "" || r.To != "" {
			t.Errorf("swallowed attribution parsed as headers: %+v", r)
		}
	}
}

func TestFoldedAttributionAndFlattenedHTML(t *testing.T) {
	for _, body := range []string{
		"Il lun 5 ott 2026\n> alle 22:07, Alex\n>> <alex@example.test> ha scritto:\n\n> Synthetic bulletin.",
		"Reply.Il lun 5 ott 2026 alle 22:07, Alex <alex@example.test> ha scritto:Synthetic bulletin.",
	} {
		blocks := Peel(body)
		r := Parse(blocks[len(blocks)-1])
		if r.DateReason != DateParsed || r.Address != "alex@example.test" || r.Block.Text != "Synthetic bulletin." {
			t.Errorf("folded/flattened attribution lost: %+v", blocks)
		}
	}
}

func TestUnknownLabelsDoNotBecomeRecipients(t *testing.T) {
	bs := Peel("From: Alex <alex@example.test>\nTo: Robin <robin@example.test>\nUnrecognised: Other <other@example.test>\nSubject: Bulletin\n\nSynthetic bulletin.")
	r := Parse(bs[0])
	if r.To != "Robin <robin@example.test>" || r.Subject != "Bulletin" {
		t.Errorf("unknown label leaked into routing: %+v", r)
	}
}

func TestHeaderFoldingDoesNotSwallowAnAttribution(t *testing.T) {
	bs := Peel("From: Robin <robin@example.test>\nDate: 6 Oct 2026 09:12\nOn Mon, 5 Oct 2026 at 22:07, Alex <alex@example.test> wrote:\nDa: Casey <casey@example.test>\nDate: 4 Oct 2026 20:00\n\nSynthetic bulletin.")
	if len(bs) != 3 || bs[1].Kind != KindAttribution || Parse(bs[1]).Address != "alex@example.test" {
		t.Errorf("header crossed the next attribution: %+v", bs)
	}
}

func TestAttributionDoesNotMistakeUppercaseAuthorForZone(t *testing.T) {
	for _, person := range []string{"BOB <bob@example.test>", "BOB"} {
		a := ParseAttribution("On 5 Oct 2026 at 22:07, " + person + " wrote:")
		if a.Sender != "BOB" || a.TZ != "" || a.DateReason != DateParsed {
			t.Errorf("author became a zone: %+v", a)
		}
	}
	a := ParseAttribution("On 5 Oct 2026 at 22:07, NZST, Alex <alex@example.test> wrote:")
	if a.TZ != "NZST" || a.Sender != "Alex" || a.DateReason != DateParsed {
		t.Errorf("stated comma-separated zone was lost: %+v", a)
	}
}

func TestMalformedExistingEnglishClockIsNotStated(t *testing.T) {
	// This shape in the anonymised English corpus previously became midnight.
	bs := Peel("On Thu, Sep 26, 2024 at 16:33 PM, Alex <alex@example.test> wrote:\n\nSynthetic bulletin.")
	if len(bs) != 1 {
		t.Fatalf("blocks=%d", len(bs))
	}
	r := Parse(bs[0])
	if !r.Sent.IsZero() || r.DateReason != DateInvalidClock || r.DatePrecision != PrecisionUnknown || r.Sender != "Alex" || r.Address != "alex@example.test" {
		t.Errorf("malformed English time became evidence or lost its author: %+v", r)
	}
}
