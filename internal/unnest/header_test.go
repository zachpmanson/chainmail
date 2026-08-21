package unnest

import (
	"strings"
	"testing"
)

func TestParseHeaderBlock(t *testing.T) {
	// The Gmail forward form: rule then keys, which Peel hands over as one
	// sentinel.
	got := ParseHeaderBlock("---------- Forwarded message ---------\n" +
		"From: Seven Culber <seven.culber@qonos.fed>\n" +
		"Date: Mon, 2 Feb 2026 at 3:35 PM\n" +
		"Subject: FW: the quarterly export\n" +
		"To: Tuvok Crusher <tuvok.crusher@ferenginar.fed>")
	if got.Sender != "Seven Culber" || got.Address != "seven.culber@qonos.fed" {
		t.Errorf("sender/address = %q / %q", got.Sender, got.Address)
	}
	if d := got.Sent.Format("2006-01-02 15:04"); d != "2026-02-02 15:35" {
		t.Errorf("sent = %q", d)
	}
	if got.Subject != "FW: the quarterly export" {
		t.Errorf("subject = %q", got.Subject)
	}
	if got.To != "Tuvok Crusher <tuvok.crusher@ferenginar.fed>" {
		t.Errorf("to = %q", got.To)
	}
}

// Apple's form: "Begin forwarded message:", a full month name, seconds, and a
// zone label, with no Subject at all.
func TestParseHeaderBlockAppleForm(t *testing.T) {
	got := ParseHeaderBlock("Begin forwarded message:\n" +
		"From: Una Picard <una.picard@daystrom.fed>\n" +
		"Date: 29 January 2026 at 4:08:58 PM NZDT\n" +
		"To: Una Crusher <una.crusher@bajor.fed>")
	if d := got.Sent.Format("2006-01-02 15:04:05"); d != "2026-01-29 16:08:58" {
		t.Errorf("sent = %q", d)
	}
	if got.TZ != "NZDT" {
		t.Errorf("tz = %q", got.TZ)
	}
	if got.Subject != "" {
		t.Errorf("invented a subject: %q", got.Subject)
	}
}

// Outlook writes "Sent:" for the same fact as "Date:", bolds its keys, and
// localises them.
func TestParseHeaderBlockOutlookAndLocalised(t *testing.T) {
	got := ParseHeaderBlock("*From:* Ro Laren <ro.laren@daystrom.fed>\n" +
		"*Sent:* Tuesday, 18 August 2026 4:47 pm\n" +
		"*To:* Jake Burnham\n*Cc:* Zora Miller\n*Subject:* the levy column")
	if got.Address != "ro.laren@daystrom.fed" {
		t.Errorf("address = %q", got.Address)
	}
	if d := got.Sent.Format("2006-01-02 15:04"); d != "2026-08-18 16:47" {
		t.Errorf("sent = %q (Sent: must fill the same field as Date:)", d)
	}
	if got.Cc != "Zora Miller" {
		t.Errorf("cc = %q", got.Cc)
	}

	de := ParseHeaderBlock("Von: Ro Laren <ro.laren@daystrom.fed>\n" +
		"Gesendet: Montag, 2 Februar 2026 15:35\nBetreff: die Abrechnung")
	if de.Address != "ro.laren@daystrom.fed" || de.Subject != "die Abrechnung" {
		t.Errorf("localised keys not mapped: %+v", de)
	}
}

// Meeting chrome is Key: value shaped. A block of it must not yield a sender.
func TestParseHeaderBlockRefusesMeetingChrome(t *testing.T) {
	got := ParseHeaderBlock("Meeting ID: 123 456 789\nPasscode: 4821\nDialin: +64 9 555 0100")
	if got.Sender != "" || got.Address != "" || !got.Sent.IsZero() {
		t.Errorf("fabricated a message from meeting chrome: %+v", got)
	}
}

// Recipients are the point of this form: they credit people who never sent
// anything and would otherwise be absent from the corpus entirely.
func TestHeaderBlockRecoversRecipientsAcrossCorpus(t *testing.T) {
	var blocks, withTo, withSender, withDate int
	for _, f := range fixtures(t) {
		for _, b := range Peel(f.Body) {
			if b.Kind == KindAttribution || b.Sentinel == "" {
				continue
			}
			h := ParseHeaderBlock(b.Sentinel)
			if h.Sender == "" && h.Address == "" && h.Sent.IsZero() && h.To == "" {
				continue
			}
			blocks++
			if h.To != "" {
				withTo++
			}
			// A name with no address still attributes the message. 34 blocks in
			// this corpus are "From: Alice | Acme" with no address at all, which
			// is a real Outlook form and not a parse failure — measuring address
			// alone would count them as misses.
			if h.Address != "" || h.Sender != "" {
				withSender++
			}
			if !h.Sent.IsZero() {
				withDate++
			}
		}
	}
	t.Logf("header blocks=%d attributable=%d (%.1f%%) date=%d (%.1f%%) to=%d (%.1f%%)",
		blocks, withSender, 100*float64(withSender)/float64(blocks),
		withDate, 100*float64(withDate)/float64(blocks),
		withTo, 100*float64(withTo)/float64(blocks))
	if blocks == 0 {
		t.Fatal("no header blocks parsed across the corpus")
	}
	if r := float64(withSender) / float64(blocks); r < 0.97 {
		t.Errorf("attributable recall %.3f below 0.97", r)
	}
	if r := float64(withDate) / float64(blocks); r < 0.97 {
		t.Errorf("date recall %.3f below 0.97", r)
	}
}

// A long recipient list wraps, and the quoted rendering loses the leading
// whitespace RFC 5322 folding would have left. Stopping the block at the wrap
// orphaned every key after it: Subject: landed in the body text and the
// recipients past the wrap vanished. 10 of 28 recovered entries on a real trail.
func TestFoldedRecipientListDoesNotTruncateTheBlock(t *testing.T) {
	body := "*From:* Bo Vantel <bo@fjord.co.nz>\r\n" +
		"*Sent:* Tuesday, 25 November 2025 1:29 pm\r\n" +
		"*To:* Ro Laren <ro@ex.fed>; Ana Quill <\r\n" +
		"ana.quill@ex.fed>\r\n" +
		"*Subject:* Fjord & Acme - Data Sharing\r\n" +
		"\r\n" +
		"Hi Ro and Ana,\r\n"
	blocks := Peel(body)
	if len(blocks) != 1 {
		t.Fatalf("got %d blocks, want 1", len(blocks))
	}
	h := ParseHeaderBlock(blocks[0].Sentinel)
	if h.Subject != "Fjord & Acme - Data Sharing" {
		t.Errorf("subject = %q — a key after the fold was orphaned", h.Subject)
	}
	// The recipient past the wrap must survive: losing them loses a participant.
	if !strings.Contains(h.To, "ana.quill@ex.fed") {
		t.Errorf("to = %q, lost the recipient after the fold", h.To)
	}
	// And the orphaned keys must not be sitting in the body.
	if strings.Contains(blocks[0].Text, "Subject:") || strings.Contains(blocks[0].Text, "ana.quill@") {
		t.Errorf("header tail leaked into the body: %q", blocks[0].Text)
	}
	if !strings.HasPrefix(blocks[0].Text, "Hi Ro and Ana,") {
		t.Errorf("body = %q", blocks[0].Text)
	}
}
