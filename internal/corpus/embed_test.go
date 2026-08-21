package corpus

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/zachpmanson/chainmail/internal/embed"
)

// Everything here is invented. This package indexes real personal
// correspondence, so committed fixtures use example.com addresses and content
// written for the test.

func embedStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func backfill(t *testing.T, s *Store, e embed.Embedder, opt BackfillOptions) BackfillReport {
	t.Helper()
	rep, err := s.BackfillEmbeddings(context.Background(), e, opt)
	if err != nil {
		t.Fatalf("backfill: %v", err)
	}
	return rep
}

// ---------------------------------------------------------------- storage

func TestVectorRoundTripsThroughTheBlobColumn(t *testing.T) {
	// Every similarity in the system is computed from these bytes, so a lossy
	// round trip would show up only as a ranking nobody can explain. The values
	// include a negative, a subnormal-ish small and an exact-power-of-two.
	want := []float32{0.5, -0.25, 1e-7, 0.123456789, -1, 0}
	blob := encodeVector(want)
	got, err := decodeVector(blob, len(want))
	if err != nil {
		t.Fatal(err)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("component %d: %v round-tripped to %v", i, want[i], got[i])
		}
	}
}

func TestStoredVectorRoundTripsThroughTheStore(t *testing.T) {
	s := embedStore(t)
	id := put(t, s, msg{id: "round-trip@example.com", subject: "Billing csv",
		body: "The nightly billing csv landed twice and both copies were processed."})

	f := embed.NewFake(16)
	backfill(t, s, f, BackfillOptions{})

	stored, err := s.Embedding(id, f.Model())
	if err != nil {
		t.Fatal(err)
	}
	if stored == nil {
		t.Fatal("entry has no stored vector")
	}
	text, reason := EmbedTextFor(SourceMail, "Billing csv",
		"The nightly billing csv landed twice and both copies were processed.")
	if reason != "" {
		t.Fatalf("fixture was skipped: %s", reason)
	}
	fresh, err := f.Embed(context.Background(), []string{text})
	if err != nil {
		t.Fatal(err)
	}
	sim, err := embed.Cosine(stored, fresh[0])
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(sim-1) > 1e-6 {
		t.Errorf("stored vector differs from a freshly computed one: cosine %v", sim)
	}
}

func TestDecodingRefusesAVectorOfTheWrongWidth(t *testing.T) {
	// A blob is only ever as trustworthy as its recorded dim. Reading three
	// floats out of a four-float blob would return a similarity computed from
	// the wrong data, which no test of the ranking could catch.
	blob := encodeVector([]float32{1, 0, 0, 0})
	if _, err := decodeVector(blob, 3); !errors.Is(err, embed.ErrDimMismatch) {
		t.Errorf("decoding 4 floats as 3: err = %v, want ErrDimMismatch", err)
	}
	if _, err := decodeVector(blob[:7], 4); !errors.Is(err, embed.ErrDimMismatch) {
		t.Errorf("decoding a truncated blob: err = %v, want ErrDimMismatch", err)
	}
}

// A query vector of the wrong width must not silently match nothing either: the
// stored rows are filtered on dim in SQL, so a mismatch is an empty result, and
// the guard against that being mistaken for "no matches" is that the model name
// and dimension are recorded and reportable.
func TestQueryVectorOfAnotherWidthMatchesNothingAndSaysSo(t *testing.T) {
	s := embedStore(t)
	put(t, s, msg{id: "width@example.com", subject: "Rates",
		body: "The unit rate on the June invoice does not match the contract."})
	f := embed.NewFake(16)
	backfill(t, s, f, BackfillOptions{})

	hits, err := s.SearchEntries(Query{
		Semantic: &SemanticQuery{Vector: make([]float32, 8), Model: f.Model(), Only: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 0 {
		t.Fatalf("an 8-wide query matched %d 16-wide vectors", len(hits))
	}
	st, err := s.EmbedStats()
	if err != nil {
		t.Fatal(err)
	}
	if len(st) != 1 || st[0].Dim != 16 {
		t.Fatalf("EmbedStats = %+v, want one model at dim 16 so the mismatch is diagnosable", st)
	}
}

// ------------------------------------------------------- text preparation

func TestQuotedHistoryIsNotEmbeddedTwice(t *testing.T) {
	// The corpus stores quoted blocks as entries of their own, so a body that
	// still carries its history would give this one entry a vector averaging
	// every message in the thread — and make every reply in the thread
	// near-identical to every other.
	body := strings.Join([]string{
		"Confirming the meter read for the Kaiapoi site.",
		"",
		"On Mon, 12 May 2025 at 09:14, Dana Ruiz <dana@example.com> wrote:",
		"> Can you send the solar export figures as well, and the",
		"> reconciliation spreadsheet for April.",
	}, "\n")
	text, reason := EmbedTextFor(SourceMail, "Meter read", body)
	if reason != "" {
		t.Fatalf("unexpected skip: %s", reason)
	}
	if !strings.Contains(text, "Kaiapoi") {
		t.Errorf("dropped the sender's own text: %q", text)
	}
	for _, quoted := range []string{"solar export", "reconciliation", "Dana Ruiz"} {
		if strings.Contains(text, quoted) {
			t.Errorf("quoted history %q survived into the embedded text: %q", quoted, text)
		}
	}
}

// A body that opens straight into a forward or an attribution relayed somebody
// else's words and added none of its own. Embedding the relayed text would give
// this entry a vector of a message that is already in the corpus under its own
// id — the same double-counting quoted history causes, arriving by a different
// route.
func TestABareRelayContributesNoWordsOfItsOwn(t *testing.T) {
	cases := []struct{ name, body string }{
		{"bare forward", "---------- Forwarded message ---------\n" +
			"From: Dana Ruiz <dana@example.com>\nSubject: Meter read\n\n" +
			"Please find the April readings attached for both of the sites.\n"},
		{"empty reply", "On Mon, 12 May 2025 at 09:14, Dana Ruiz <dana@example.com> wrote:\n" +
			"> the readings are attached and the totals look right to me\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// A long subject, so the fixture fails the gate for the reason under
			// test rather than for want of a few words.
			text, reason := EmbedTextFor(SourceMail,
				"Meter read for the two Kaiapoi sites, April", c.body)
			if reason != "" {
				t.Fatalf("unexpected skip: %s", reason)
			}
			if strings.Contains(text, "readings") {
				t.Errorf("relayed text was embedded as this entry's own: %q", text)
			}
			if !strings.Contains(text, "Kaiapoi") {
				t.Errorf("subject lost: %q", text)
			}
		})
	}
	// With nothing but a short subject, such an entry has no topic at all.
	if _, reason := EmbedTextFor(SourceMail, "Meter read",
		"---------- Forwarded message ---------\nFrom: Dana Ruiz <dana@example.com>\n\n"+
			"Please find the April readings attached for both of the sites.\n"); reason != skipShort {
		t.Errorf("reason = %q, want %q", reason, skipShort)
	}
}

func TestSlackBodiesAreEmbeddedWhole(t *testing.T) {
	// A Slack message is already atomic, and its ">" lines are someone quoting
	// for emphasis rather than a client nesting a forward. Peeling them would
	// throw away the message.
	body := "> the invoice total\nis wrong again, second month running"
	text, reason := EmbedTextFor(SourceSlack, "", body)
	if reason != "" {
		t.Fatalf("unexpected skip: %s", reason)
	}
	if !strings.Contains(text, "invoice total") || !strings.Contains(text, "second month") {
		t.Errorf("Slack body was peeled: %q", text)
	}
}

func TestSubjectIsEmbeddedWithoutItsReplyPrefixes(t *testing.T) {
	text, reason := EmbedTextFor(SourceMail, "RE: Fwd: Re: Billing csv ingestion",
		"See attached, the counts are still off by one day.")
	if reason != "" {
		t.Fatalf("unexpected skip: %s", reason)
	}
	if !strings.HasPrefix(text, "Billing csv ingestion") {
		t.Errorf("reply prefixes not stripped: %q", text)
	}
}

func TestAURLIsReducedToItsHost(t *testing.T) {
	// The host is evidence of who a thread is with; the signed path is
	// high-entropy noise that eats the token budget.
	text, reason := EmbedTextFor(SourceSlack, "",
		"figures are at https://files.example.com/d/9f3a1c?sig=AAAABBBBCCCC&exp=1755 please review")
	if reason != "" {
		t.Fatalf("unexpected skip: %s", reason)
	}
	if !strings.Contains(text, "files.example.com") {
		t.Errorf("host lost: %q", text)
	}
	if strings.Contains(text, "AAAABBBBCCCC") || strings.Contains(text, "9f3a1c") {
		t.Errorf("signed path survived: %q", text)
	}
}

func TestEntriesWithNoTopicAreNotEmbedded(t *testing.T) {
	cases := []struct {
		name, subject, body, want string
	}{
		{"empty", "", "", skipEmpty},
		{"file only slack post", "", "   ", skipEmpty},
		{"one word reply", "", "thanks", skipShort},
		{"short ack", "", "thanks, will do", skipShort},
		{"digits only", "", "1 2 3 4 5 6 7 8 9 10", skipShort},
		{"tombstone", "", "This message was deleted.", skipShort},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			text, reason := EmbedTextFor(SourceSlack, c.subject, c.body)
			if reason != c.want {
				t.Errorf("reason = %q, want %q (text %q)", reason, c.want, text)
			}
			if text != "" {
				t.Errorf("a skipped entry must carry no text, got %q", text)
			}
		})
	}
}

// The gate has to be recorded, not merely obeyed: without a row, every future
// run reconsiders the third of the corpus that has nothing worth embedding.
func TestASkippedEntryIsRecordedSoItIsNotReconsidered(t *testing.T) {
	s := embedStore(t)
	put(t, s, msg{id: "short@example.com", body: "thanks"})
	f := embed.NewFake(16)

	first := backfill(t, s, f, BackfillOptions{})
	if first.Skipped != 1 || first.Embedded != 0 {
		t.Fatalf("first run: %+v, want one skip and no vectors", first)
	}
	if f.Calls != 0 {
		t.Errorf("the model was asked about %d texts it should never see", f.Calls)
	}
	second := backfill(t, s, f, BackfillOptions{})
	if second.Pending != 0 {
		t.Errorf("second run found %d pending; a skip must be durable", second.Pending)
	}
}

func TestNoiseIsNeverSentToTheModel(t *testing.T) {
	// Bots and join/leave chatter are a third of the Slack half of this corpus
	// and are excluded from every default search anyway, so embedding them
	// spends model time on vectors nothing will ever rank.
	s := embedStore(t)
	slackMsg(t, s, "slack:C123:1.1", "Wiremu Clarke has joined the channel", june, false, "channel_join")
	slackMsg(t, s, "slack:C123:1.2", "Deploy 4471 finished, all checks green", june, true, "")
	real := slackMsg(t, s, "slack:C123:1.3",
		"the reconciliation is out by one day again, same as last month", june, false, "")

	f := embed.NewFake(16)
	rep := backfill(t, s, f, BackfillOptions{})
	if rep.Embedded != 1 || rep.Skipped != 0 {
		t.Fatalf("%+v, want exactly the one real message embedded", rep)
	}
	if v, err := s.Embedding(real, f.Model()); err != nil || v == nil {
		t.Errorf("the real message has no vector: %v %v", v, err)
	}
}

// ---------------------------------------------------------------- staleness

func TestChangedTextIsReembeddedAndUnchangedTextIsNot(t *testing.T) {
	s := embedStore(t)
	stable := msg{id: "stable@example.com", subject: "Contract dates",
		body: "The contract start date on the schedule is a month out."}
	revised := msg{id: "revised@example.com", subject: "Meter read",
		body: "The Kaiapoi read is 4471 as at the end of the month."}
	stableID := put(t, s, stable)
	revisedID := put(t, s, revised)

	f := embed.NewFake(16)
	if rep := backfill(t, s, f, BackfillOptions{}); rep.Embedded != 2 {
		t.Fatalf("first run embedded %d, want 2", rep.Embedded)
	}
	before, err := s.Embedding(stableID, f.Model())
	if err != nil {
		t.Fatal(err)
	}

	// A re-ingest with identical content, and one whose body actually moved.
	put(t, s, stable)
	revised.body = "Correction: the Kaiapoi read is 4517, not 4471."
	put(t, s, revised)

	f.Calls, f.Seen = 0, nil
	rep := backfill(t, s, f, BackfillOptions{})
	if rep.Embedded != 1 {
		t.Fatalf("second run embedded %d, want only the revised entry (%+v)", rep.Embedded, rep)
	}
	if len(f.Seen) != 1 || !strings.Contains(f.Seen[0], "4517") {
		t.Errorf("the model was asked about %v, want only the revised text", f.Seen)
	}
	after, err := s.Embedding(stableID, f.Model())
	if err != nil {
		t.Fatal(err)
	}
	if sim, err := embed.Cosine(before, after); err != nil || math.Abs(sim-1) > 1e-9 {
		t.Errorf("untouched entry's vector moved: cosine %v %v", sim, err)
	}
	newVec, err := s.Embedding(revisedID, f.Model())
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := f.Embed(context.Background(), []string{f.Seen[0]})
	if err != nil {
		t.Fatal(err)
	}
	if sim, err := embed.Cosine(newVec, fresh[0]); err != nil || math.Abs(sim-1) > 1e-6 {
		t.Errorf("revised entry kept a stale vector: cosine %v %v", sim, err)
	}
}

// A model change must be a background migration, not an outage: the old vectors
// stay searchable until the new set is complete.
func TestAModelChangeLeavesTheOldVectorsSearchable(t *testing.T) {
	s := embedStore(t)
	put(t, s, msg{id: "one@example.com", subject: "Ingestion",
		body: "The nightly ingestion dropped a whole day of readings."})
	old := embed.NewFake(16)
	backfill(t, s, old, BackfillOptions{})

	newer := &embed.Fake{Name: "fake-other", Dimension: 16}
	backfill(t, s, newer, BackfillOptions{})

	for _, m := range []string{old.Model(), newer.Model()} {
		hits, err := s.SearchEntries(Query{Semantic: &SemanticQuery{
			Vector: unitQuery(t, 16), Model: m, Only: true}})
		if err != nil {
			t.Fatal(err)
		}
		if len(hits) != 1 {
			t.Errorf("model %s: %d hits, want 1 — both generations must answer", m, len(hits))
		}
	}
	n, err := s.PruneEmbeddings(newer.Model())
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("pruned %d superseded rows, want 1", n)
	}
	hits, err := s.SearchEntries(Query{Semantic: &SemanticQuery{
		Vector: unitQuery(t, 16), Model: old.Model(), Only: true}})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 0 {
		t.Errorf("pruned model still answers with %d hits", len(hits))
	}
}

// -------------------------------------------------------------- resumability

// Embedding a real corpus takes minutes, so an interrupted run has to be worth
// nothing more than the batch it was in the middle of — no duplicated work, and
// no entry silently left without a vector.
func TestBackfillResumesWithoutDuplicatingOrSkippingWork(t *testing.T) {
	s := embedStore(t)
	const n = 25
	ids := make([]int64, 0, n)
	for i := range n {
		// Every body is distinct, so "was this text asked for twice" is a
		// question about the backfill rather than about the fixture.
		ids = append(ids, put(t, s, msg{
			id:      fmt.Sprintf("resume-%02d@example.com", i),
			subject: fmt.Sprintf("Reconciliation row %02d", i),
			body:    fmt.Sprintf("Row %02d of the ledger needs checking against the schedule.", i),
		}))
	}

	f := embed.NewFake(16)
	// Interrupt after two batches by refusing to embed any further.
	partial := backfill(t, s, f, BackfillOptions{Batch: 4, Limit: 8})
	if partial.Embedded != 8 {
		t.Fatalf("partial run embedded %d, want 8", partial.Embedded)
	}
	firstPass := append([]string(nil), f.Seen...)

	f.Seen = nil
	rest := backfill(t, s, f, BackfillOptions{Batch: 4})
	if rest.Embedded != n-8 {
		t.Errorf("resumed run embedded %d, want %d", rest.Embedded, n-8)
	}
	// No duplicated work: nothing the first pass finished is asked for again.
	done := map[string]bool{}
	for _, t := range firstPass {
		done[t] = true
	}
	for _, text := range f.Seen {
		if done[text] {
			t.Errorf("resumed run re-embedded %q", text)
		}
	}
	// No gaps: every entry ends with a vector.
	for _, id := range ids {
		v, err := s.Embedding(id, f.Model())
		if err != nil {
			t.Fatal(err)
		}
		if v == nil {
			t.Errorf("entry %d has no vector after a resumed backfill", id)
		}
	}
	// And a third run has nothing to do.
	if final := backfill(t, s, f, BackfillOptions{}); final.Pending != 0 {
		t.Errorf("third run found %d pending", final.Pending)
	}
}

// The work has to be taken in a fixed order, or an interrupted run and its
// resumption cannot be reasoned about at all.
func TestBackfillTakesEntriesInIDOrder(t *testing.T) {
	s := embedStore(t)
	var want []string
	for i := range 6 {
		body := fmt.Sprintf("Item %02d on the schedule needs a second look.", i)
		put(t, s, msg{id: fmt.Sprintf("order-%02d@example.com", i), body: body})
		want = append(want, body)
	}
	f := embed.NewFake(16)
	backfill(t, s, f, BackfillOptions{Batch: 2})
	if len(f.Seen) != len(want) {
		t.Fatalf("saw %d texts, want %d", len(f.Seen), len(want))
	}
	for i := range want {
		if f.Seen[i] != want[i] {
			t.Fatalf("text %d was %q, want %q — order is not by id", i, f.Seen[i], want[i])
		}
	}
}

// A failing model must leave the corpus exactly as it was, so the retry is the
// same work rather than a partially-written batch nobody can characterise.
func TestAFailedBatchWritesNothing(t *testing.T) {
	s := embedStore(t)
	id := put(t, s, msg{id: "fail@example.com",
		body: "The export never arrived, please resend it when you can."})
	f := embed.NewFake(16)
	f.Fail = errors.New("model went away")

	_, err := s.BackfillEmbeddings(context.Background(), f, BackfillOptions{})
	if err == nil {
		t.Fatal("want the model's error to surface")
	}
	v, err := s.Embedding(id, f.Model())
	if err != nil {
		t.Fatal(err)
	}
	if v != nil {
		t.Error("a failed batch left a vector behind")
	}

	f.Fail = nil
	if rep := backfill(t, s, f, BackfillOptions{}); rep.Embedded != 1 {
		t.Errorf("retry embedded %d, want 1", rep.Embedded)
	}
}

// unitQuery is a query vector every stored vector has some similarity to, for
// tests that only care whether a row was reachable at all.
func unitQuery(t *testing.T, dim int) []float32 {
	t.Helper()
	v := make([]float32, dim)
	for i := range v {
		v[i] = 1
	}
	if err := embed.Normalise(v); err != nil {
		t.Fatal(err)
	}
	return v
}
