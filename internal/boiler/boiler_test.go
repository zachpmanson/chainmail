package boiler

import (
	"fmt"
	"testing"
)

// Every block here is invented. The shapes are the ones a real corpus holds — a
// sign-off with a title and a number under it, a four-line confidentiality
// notice appended by an organisation — but no line of real correspondence is
// committed, and every name and domain is made up.

const (
	ada   = int64(1)
	grace = int64(2)
	tom   = int64(3)
)

var adaSig = []string{"Ada Byron", "Head of Metering, Loomworks", "+61 400 000 000"}

func msg(id, author int64, domain string, lines ...string) Message {
	return Message{ID: id, Author: author, Domain: domain, Lines: lines}
}

func TestARepeatedTailIsFoldedAndTheSameLinesElsewhereAreNot(t *testing.T) {
	// The whole claim: a block is boilerplate because of who repeats it, not
	// because of what it says. Grace pastes Ada's details into one message of her
	// own — asking who to call — and that is prose.
	var msgs []Message
	for i := 0; i < 4; i++ {
		msgs = append(msgs, msg(int64(10+i), ada, "loomworks.example",
			append([]string{fmt.Sprintf("Point %d.", i)}, adaSig...)...))
	}
	msgs = append(msgs, msg(99, grace, "fjordvik.example",
		append([]string{"Ring the metering lead:"}, adaSig...)...))

	got := Detect(msgs, Default())
	if f := got[10]; f.Lines != 3 || f.Scope != Author {
		t.Errorf("Ada's fold = %+v, want 3 lines at author scope", f)
	}
	if f, ok := got[99]; ok {
		t.Errorf("Grace's message folded %+v; the same lines from another author are prose", f)
	}
}

func TestTheLongestRepeatedTailWinsRatherThanAFixedWindow(t *testing.T) {
	// Ada lengthened her signature partway through. A fixed five-line window
	// would cut the short one's closing sentence off and leave two lines of the
	// long one on screen; growing the tail until the repetition stops finds each
	// block's own length.
	short := []string{"Ada Byron", "Loomworks"}
	long := []string{"Ada Byron", "Head of Metering, Loomworks", "+61 400 000 000",
		"loomworks.example", "Formerly Fjordvik Metering"}
	var msgs []Message
	for i := 0; i < 3; i++ {
		msgs = append(msgs, msg(int64(10+i), ada, "loomworks.example",
			append([]string{"Early note."}, short...)...))
	}
	for i := 0; i < 3; i++ {
		msgs = append(msgs, msg(int64(20+i), ada, "loomworks.example",
			append([]string{"Later note."}, long...)...))
	}
	got := Detect(msgs, Default())
	if f := got[10]; f.Lines != 2 {
		t.Errorf("early fold = %+v, want the 2-line signature", f)
	}
	// 5 and not 2: the long block ends in different lines, so the two-line tail
	// they share is not a tail of the long messages at all.
	if f := got[20]; f.Lines != 5 {
		t.Errorf("later fold = %+v, want the whole 5-line signature", f)
	}
}

func TestATailSeenTwiceIsNotYetBoilerplate(t *testing.T) {
	// Two is one repetition, and one repetition of a short sign-off is ordinary
	// coincidence. The third message is what settles it.
	two := []Message{
		msg(10, ada, "loomworks.example", "Sent the file.", "Thanks", "Ada"),
		msg(11, ada, "loomworks.example", "Sent the other one.", "Thanks", "Ada"),
	}
	if f, ok := Detect(two, Default())[10]; ok {
		t.Errorf("folded %+v on two messages; the threshold is three", f)
	}
	three := append(two, msg(12, ada, "loomworks.example", "And the last.", "Thanks", "Ada"))
	if f := Detect(three, Default())[10]; f.Lines != 2 || f.Scope != Author {
		t.Errorf("fold = %+v, want the 2-line sign-off at author scope", f)
	}
}

func TestAnOrgNoticeIsFoundAcrossSendersAtOneDomain(t *testing.T) {
	// The case the author pass cannot reach: three people at one domain, none of
	// them sending enough for their own signature to repeat, all of them carrying
	// the same notice.
	notice := []string{
		"This email and any attachments are confidential.",
		"If you are not the intended recipient, delete it and tell us.",
		"Views expressed are the author's and not necessarily Fjordvik's.",
	}
	msgs := []Message{
		msg(10, ada, "fjordvik.example", append([]string{"Rates attached."}, notice...)...),
		msg(11, grace, "fjordvik.example", append([]string{"Signed."}, notice...)...),
		msg(12, tom, "fjordvik.example", append([]string{"Chasing the meter read."}, notice...)...),
	}
	f := Detect(msgs, Default())[11]
	if f.Scope != Domain || f.Lines != 3 || f.Senders != 3 {
		t.Errorf("fold = %+v, want a 3-line domain notice over 3 senders", f)
	}
}

func TestOneSendersRepeatIsNotADomainNotice(t *testing.T) {
	// Three messages at a domain from one mailbox is that person's signature, and
	// the author pass owns it. Calling it a domain notice would label a personal
	// sign-off as the organisation's and say so in the summary.
	var msgs []Message
	for i := 0; i < 3; i++ {
		msgs = append(msgs, msg(int64(10+i), ada, "loomworks.example",
			append([]string{"A point."}, adaSig...)...))
	}
	// The author pass is disabled so that only a domain verdict can appear.
	got := Detect(msgs, Rules{AuthorRepeats: 99})
	if f, ok := got[10]; ok {
		t.Errorf("fold = %+v, want none: one sender is not an organisation", f)
	}
}

func TestABodyThatIsEntirelyBoilerplateKeepsALine(t *testing.T) {
	// An automated alert whose whole body is a template. Folding all of it would
	// leave a bubble whose only content is a closed disclosure, which reads as a
	// page that failed to render.
	tpl := []string{"Monitor is up.", "Best,", "Fjordvik Status"}
	var msgs []Message
	for i := 0; i < 5; i++ {
		msgs = append(msgs, msg(int64(10+i), tom, "status.fjordvik.example", tpl...))
	}
	f := Detect(msgs, Default())[10]
	if f.Lines != len(tpl)-1 {
		t.Errorf("fold = %+v, want %d lines so one stays in view", f, len(tpl)-1)
	}
}

func TestASingleMessageFoldsNothing(t *testing.T) {
	got := Detect([]Message{msg(10, ada, "loomworks.example",
		append([]string{"One and only."}, adaSig...)...)}, Default())
	if len(got) != 0 {
		t.Errorf("Detect = %v, want nothing folded on one message", got)
	}
}

func TestNoEvidenceIsNotAnError(t *testing.T) {
	for _, msgs := range [][]Message{nil, {}, {msg(10, 0, "", "Anonymous.")}} {
		if got := Detect(msgs, Default()); len(got) != 0 {
			t.Errorf("Detect(%v) = %v, want nothing", msgs, got)
		}
	}
}

func TestAShortTailIsLeftAloneBecauseTheControlCostsALine(t *testing.T) {
	// A one-line tail trades a line of text for a line of chrome, so MinLines is
	// two and a repeated single name is not folded.
	var msgs []Message
	for i := 0; i < 6; i++ {
		msgs = append(msgs, msg(int64(10+i), ada, "loomworks.example",
			fmt.Sprintf("Point %d.", i), "Ada"))
	}
	if f, ok := Detect(msgs, Default())[10]; ok {
		t.Errorf("fold = %+v, want none: one line is below MinLines", f)
	}
}
