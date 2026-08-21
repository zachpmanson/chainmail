package corpus

import (
	"context"
	"testing"
	"time"

	"github.com/zachpmanson/chainmail/internal/embed"
)

// Everything here is invented. This package indexes real personal
// correspondence, so committed fixtures use example.com addresses and content
// written for the test.

// pin returns a fake whose vector for one entry's prepared text is exactly the
// one given, so a test can state the geometry it means rather than discover
// whatever a hashed bag of words happens to produce.
func pin(t *testing.T, dim int) *embed.Fake {
	t.Helper()
	return &embed.Fake{Name: "pinned", Dimension: dim, Texts: map[string][]float32{}}
}

func pinText(t *testing.T, f *embed.Fake, source, subject, body string, v []float32) {
	t.Helper()
	text, reason := EmbedTextFor(source, subject, body)
	if reason != "" {
		t.Fatalf("fixture %q was skipped as %s", subject, reason)
	}
	f.Texts[text] = v
}

// queryVector embeds a query string the way the CLI does.
func queryVector(t *testing.T, f *embed.Fake, text string) []float32 {
	t.Helper()
	vs, err := f.Embed(context.Background(), []string{text})
	if err != nil {
		t.Fatal(err)
	}
	return vs[0]
}

// The point of the whole feature: the topic word appears nowhere in the mail,
// so no lexical index can reach it and only the vectors can.
func TestSemanticSearchBridgesTheVocabularyGap(t *testing.T) {
	s := embedStore(t)
	const dim = 8
	f := pin(t, dim)

	onTopic := msg{id: "gap-on@example.com", subject: "Nightly csv",
		body: "The billing csv from Tuesday landed twice and both copies were loaded."}
	offTopic := msg{id: "gap-off@example.com", subject: "Team lunch",
		body: "Booking the back room at the usual place for Friday, twelve of us."}
	put(t, s, onTopic)
	put(t, s, offTopic)

	pinText(t, f, SourceMail, onTopic.subject, onTopic.body, embed.Mix(dim, 0, 1, 0.2))
	pinText(t, f, SourceMail, offTopic.subject, offTopic.body, embed.Unit(dim, 1, 1))
	f.Texts["ingestion"] = embed.Unit(dim, 0, 1)
	backfill(t, s, f, BackfillOptions{})

	// "ingestion" is in neither body, so the lexical half finds nothing at all.
	lexical, err := s.SearchEntries(Query{Text: "ingestion"})
	if err != nil {
		t.Fatal(err)
	}
	if len(lexical) != 0 {
		t.Fatalf("lexical search found %d hits for a word in no body", len(lexical))
	}

	hits, err := s.SearchEntries(Query{Text: "ingestion", Semantic: &SemanticQuery{
		Vector: queryVector(t, f, "ingestion"), Model: f.Model()}})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 {
		t.Fatal("semantic search found nothing for a topic the corpus discusses")
	}
	if hits[0].ExtID != "mail:"+onTopic.id {
		t.Errorf("top hit is %s, want the csv thread", hits[0].ExtID)
	}
	if hits[0].SemRank == 0 {
		t.Error("a hit no keyword explains must carry a SemRank, or nobody can tell why it is there")
	}
	if hits[0].Snippet == "" {
		t.Error("a semantic hit needs an excerpt: FTS5 has no matched term to highlight")
	}
}

// RRF's reason for existing: two rankings agreeing on an entry beats one
// ranking liking it more. bm25 is unbounded and negative, cosine sits in
// [-1, 1], so any weighted sum of the two needs a constant somebody tunes per
// query — and the agreement signal is the one that survives not tuning it.
func TestAgreementBetweenRankingsOutranksOneStrongRanking(t *testing.T) {
	s := embedStore(t)
	const dim = 8
	f := pin(t, dim)

	// Both bodies contain the query word. Only one of them is also near the
	// query vector.
	both := msg{id: "fuse-both@example.com", subject: "Reconciliation",
		body: "The reconciliation for June is short by one day of readings."}
	lexicalOnly := msg{id: "fuse-lex@example.com", subject: "Reconciliation",
		body: "Reconciliation reconciliation reconciliation, per the reconciliation policy."}
	put(t, s, both)
	put(t, s, lexicalOnly)

	pinText(t, f, SourceMail, both.subject, both.body, embed.Unit(dim, 0, 1))
	pinText(t, f, SourceMail, lexicalOnly.subject, lexicalOnly.body, embed.Unit(dim, 3, 1))
	f.Texts["reconciliation"] = embed.Unit(dim, 0, 1)
	backfill(t, s, f, BackfillOptions{})

	// TopK 1 is what makes "found by only one ranking" reachable at all: every
	// entry has some cosine to any query, so an untruncated vector ranking finds
	// everything.
	hits, err := s.SearchEntries(Query{Text: "reconciliation", Semantic: &SemanticQuery{
		Vector: queryVector(t, f, "reconciliation"), Model: f.Model(), TopK: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 {
		t.Fatalf("got %d hits, want 2", len(hits))
	}
	// The one only the lexical index found is the *better* keyword match: it has
	// the term four times to the winner's one. Agreement beats strength.
	if hits[1].ProseRank != 1 {
		t.Errorf("second hit's prose rank is %d, want 1 — the fixture no longer tests agreement",
			hits[1].ProseRank)
	}
	if hits[0].ExtID != "mail:"+both.id {
		t.Fatalf("top hit is %s, want the entry both rankings found", hits[0].ExtID)
	}
	if hits[0].ProseRank == 0 || hits[0].SemRank == 0 {
		t.Errorf("top hit should be found by both: prose %d, sem %d",
			hits[0].ProseRank, hits[0].SemRank)
	}
	if hits[1].SemRank != 0 {
		t.Errorf("second hit should be found by one ranking only: prose %d, sem %d",
			hits[1].ProseRank, hits[1].SemRank)
	}
	if !(hits[0].Score > hits[1].Score) {
		t.Errorf("scores do not separate: %v vs %v", hits[0].Score, hits[1].Score)
	}
}

// The noise filter's predicates are all over slack_detail, which is outer-joined
// and therefore all-NULL on a mail row. Without the coalesce, `not (NULL or
// NULL)` is NULL, WHERE reads that as false, and every mail entry disappears
// from the vector scan — not ranked lower, gone. The failure leaves no trace in
// the output, so it is asserted rather than reviewed.
func TestMailSurvivesTheSemanticScansNoiseFilter(t *testing.T) {
	s := embedStore(t)
	const dim = 8
	f := pin(t, dim)

	m := msg{id: "coalesce@example.com", subject: "Contract dates",
		body: "The contract start on the schedule is a month later than we agreed."}
	put(t, s, m)
	slackMsg(t, s, "slack:C123:2.1",
		"the contract start date on the schedule looks wrong to me too", june, false, "")
	pinText(t, f, SourceMail, m.subject, m.body, embed.Unit(dim, 0, 1))
	backfill(t, s, f, BackfillOptions{})

	// Semantic only, so nothing but the vector scan can put a row in the result.
	for _, q := range []Query{
		{Semantic: &SemanticQuery{Vector: unitQuery(t, dim), Model: f.Model(), Only: true}},
		{Text: "contract", Semantic: &SemanticQuery{Vector: unitQuery(t, dim), Model: f.Model()}},
		{Sources: []string{SourceMail}, Semantic: &SemanticQuery{
			Vector: unitQuery(t, dim), Model: f.Model(), Only: true}},
	} {
		hits, err := s.SearchEntries(q)
		if err != nil {
			t.Fatal(err)
		}
		var sawMail bool
		for _, h := range hits {
			if h.ExtID == "mail:"+m.id {
				sawMail = true
			}
		}
		if !sawMail {
			t.Fatalf("mail vanished from a semantic search: %d hits, none of them mail", len(hits))
		}
	}
}

// Structural filters are the half of a real question that a similarity score
// has no business encoding, so they apply to the vector scan exactly as they do
// to the lexical indexes — and in SQL, so the scan reads fewer rows rather than
// the same rows and then discards them.
func TestSemanticSearchHonoursTheStructuralFilters(t *testing.T) {
	s := embedStore(t)
	const dim = 8
	f := pin(t, dim)

	alice := searchPerson(t, s, "Alice Fenn", "alice@example.com")
	bob := searchPerson(t, s, "Bob Naera", "bob@example.com")

	early := msg{id: "filt-early@example.com", subject: "Schedule",
		body: "The schedule for the second quarter is attached for review.",
		ts:   may, person: alice}
	late := msg{id: "filt-late@example.com", subject: "Schedule",
		body: "The schedule for the third quarter is attached for review.",
		ts:   july, person: bob}
	put(t, s, early)
	put(t, s, late)
	pinText(t, f, SourceMail, early.subject, early.body, embed.Unit(dim, 0, 1))
	pinText(t, f, SourceMail, late.subject, late.body, embed.Unit(dim, 0, 1))
	backfill(t, s, f, BackfillOptions{})

	sem := func() *SemanticQuery {
		return &SemanticQuery{Vector: embed.Unit(dim, 0, 1), Model: f.Model(), Only: true}
	}
	all, err := s.SearchEntries(Query{Semantic: sem()})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("unfiltered semantic search returned %d, want 2", len(all))
	}

	since, err := s.SearchEntries(Query{
		Since: time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC), Semantic: sem()})
	if err != nil {
		t.Fatal(err)
	}
	if len(since) != 1 || since[0].ExtID != "mail:"+late.id {
		t.Errorf("-since ignored by the vector scan: %+v", since)
	}

	byPerson, err := s.SearchEntries(Query{People: []string{"alice@example.com"}, Semantic: sem()})
	if err != nil {
		t.Fatal(err)
	}
	if len(byPerson) != 1 || byPerson[0].ExtID != "mail:"+early.id {
		t.Errorf("author filter ignored by the vector scan: %+v", byPerson)
	}
}

// Ranking is by descending similarity, and ties break by id, because a ranking
// that wobbles between runs is indistinguishable from a bug.
func TestSemanticRankingIsOrderedAndStable(t *testing.T) {
	s := embedStore(t)
	const dim = 8
	f := pin(t, dim)

	angles := []float64{0.1, 0.6, 1.2}
	var ids []string
	for i, theta := range angles {
		m := msg{
			id:      "rank-" + string(rune('a'+i)) + "@example.com",
			subject: "Item " + string(rune('A'+i)),
			body:    "Body " + string(rune('A'+i)) + " of the schedule, for review and comment.",
		}
		put(t, s, m)
		pinText(t, f, SourceMail, m.subject, m.body, embed.Mix(dim, 0, 1, theta))
		ids = append(ids, "mail:"+m.id)
	}
	backfill(t, s, f, BackfillOptions{})

	q := Query{Semantic: &SemanticQuery{
		Vector: embed.Unit(dim, 0, 1), Model: f.Model(), Only: true}}
	first, err := s.SearchEntries(q)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != len(ids) {
		t.Fatalf("got %d hits, want %d", len(first), len(ids))
	}
	for i, h := range first {
		if h.ExtID != ids[i] {
			t.Errorf("position %d is %s, want %s (similarity %v)", i, h.ExtID, ids[i], h.Similarity)
		}
		if i > 0 && !(first[i-1].Similarity >= h.Similarity) {
			t.Errorf("similarity is not descending: %v then %v", first[i-1].Similarity, h.Similarity)
		}
	}
	second, err := s.SearchEntries(q)
	if err != nil {
		t.Fatal(err)
	}
	for i := range first {
		if first[i].ExtID != second[i].ExtID {
			t.Fatalf("ranking is not stable across runs at position %d", i)
		}
	}
}

// Chain aggregation is the headline result, and it must work off semantic hits
// too: the ranking is the same harmonic damping over whatever candidates the
// fusion produced.
func TestChainsRankFromSemanticHits(t *testing.T) {
	s := embedStore(t)
	const dim = 8
	f := pin(t, dim)

	root := msg{id: "chain-root@example.com", subject: "Ledger",
		body: "Opening the question of the ledger split across two accounts."}
	reply := msg{id: "chain-reply@example.com", parent: root.id, subject: "Re: Ledger",
		body: "Agreed, the ledger split is the reason the totals never line up."}
	other := msg{id: "chain-other@example.com", subject: "Parking",
		body: "The parking arrangements change from the first of next month."}
	put(t, s, root)
	put(t, s, reply)
	put(t, s, other)
	if _, err := s.ResolveParents(); err != nil {
		t.Fatal(err)
	}
	pinText(t, f, SourceMail, root.subject, root.body, embed.Unit(dim, 0, 1))
	pinText(t, f, SourceMail, reply.subject, reply.body, embed.Mix(dim, 0, 1, 0.1))
	pinText(t, f, SourceMail, other.subject, other.body, embed.Unit(dim, 4, 1))
	backfill(t, s, f, BackfillOptions{})

	chains, err := s.SearchChains(Query{Semantic: &SemanticQuery{
		Vector: embed.Unit(dim, 0, 1), Model: f.Model(), Only: true}})
	if err != nil {
		t.Fatal(err)
	}
	if len(chains) == 0 {
		t.Fatal("no chains from a semantic-only query")
	}
	if chains[0].RootExtID != "mail:"+root.id {
		t.Errorf("top chain root is %s, want %s", chains[0].RootExtID, "mail:"+root.id)
	}
	if chains[0].Matched != 2 || chains[0].Entries != 2 {
		t.Errorf("chain matched %d of %d, want 2 of 2", chains[0].Matched, chains[0].Entries)
	}
}

// A semantic query against a corpus with no vectors at all returns nothing, and
// that is a different thing from a failure. It is the state of the corpus before
// a backfill, so it has to be a clean empty rather than an error.
func TestSemanticSearchOnAnUnembeddedCorpusIsEmptyNotBroken(t *testing.T) {
	s := embedStore(t)
	put(t, s, msg{id: "none@example.com", subject: "Schedule",
		body: "The schedule for the quarter is attached, as discussed."})

	hits, err := s.SearchEntries(Query{Semantic: &SemanticQuery{
		Vector: unitQuery(t, 8), Model: "fake-8", Only: true}})
	if err != nil {
		t.Fatalf("unembedded corpus: %v", err)
	}
	if len(hits) != 0 {
		t.Errorf("got %d hits from a corpus with no vectors", len(hits))
	}
	// The lexical half must still answer, so a missing backfill degrades rather
	// than breaks.
	lexical, err := s.SearchEntries(Query{Text: "schedule", Semantic: &SemanticQuery{
		Vector: unitQuery(t, 8), Model: "fake-8"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(lexical) != 1 {
		t.Errorf("hybrid search lost the lexical half: %d hits", len(lexical))
	}
}
