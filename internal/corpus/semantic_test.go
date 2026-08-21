package corpus

import (
	"context"
	"testing"

	"github.com/zachpmanson/chainmail/internal/embed"
)

// Everything here is invented. This package indexes real personal
// correspondence, so committed fixtures use example.com addresses and content
// written for the test.

func TestBumpingPrepStalesEveryStoredVector(t *testing.T) {
	// The whole reason prep exists: improving what the model is shown changes
	// every vector while leaving every body_sha identical, so hashes cannot
	// detect it and the corpus would keep serving the old geometry forever.
	s := embedStore(t)
	put(t, s, msg{id: "prep-a@example.com", subject: "Billing csv",
		body: "The nightly billing csv landed twice and both copies were loaded."})
	put(t, s, msg{id: "prep-b@example.com", subject: "Meter read",
		body: "The estimated read came through low again, so the next bill will spike."})

	f := embed.NewFake(16)
	first := backfill(t, s, f, BackfillOptions{})
	if first.Embedded != 2 {
		t.Fatalf("first backfill embedded %d entries, want 2", first.Embedded)
	}

	// Rewind the stored prep to stand for vectors written by an older
	// derivation. Bodies and hashes are untouched.
	if _, err := s.db.Exec(`update entry_embeddings set prep = ?`, prepVersion-1); err != nil {
		t.Fatal(err)
	}

	stats, err := s.EmbedStats()
	if err != nil {
		t.Fatal(err)
	}
	if len(stats) != 1 {
		t.Fatalf("stats cover %d models, want 1", len(stats))
	}
	if stats[0].Stale != 2 || stats[0].Eligible != 2 {
		t.Errorf("after the bump: %d stale, %d eligible; want 2 and 2",
			stats[0].Stale, stats[0].Eligible)
	}

	before := f.Calls
	second := backfill(t, s, f, BackfillOptions{})
	if second.Embedded != 2 {
		t.Errorf("re-embed covered %d entries, want both", second.Embedded)
	}
	if f.Calls-before != 2 {
		t.Errorf("the model was asked about %d texts, want 2", f.Calls-before)
	}

	after, err := s.EmbedStats()
	if err != nil {
		t.Fatal(err)
	}
	if after[0].Stale != 0 || after[0].Eligible != 0 {
		t.Errorf("after the re-embed: %d stale, %d eligible; want none of either",
			after[0].Stale, after[0].Eligible)
	}
}

func TestTheFloorComesFromTheModelAndCanBeOverridden(t *testing.T) {
	// A floor is a statement about one model's score distribution, so it has to
	// arrive with the model rather than from a constant in this package. The
	// override matters just as much: measuring where a floor belongs requires
	// asking for no floor at all, which a plain float field could not express.
	calibrated := &embed.Fake{Name: embed.DefaultModel, Dimension: 8}
	sem, err := SemanticFor(context.Background(), calibrated, "duplicate imports", SemanticOptions{})
	if err != nil {
		t.Fatal(err)
	}
	want := embed.TraitsFor(embed.DefaultModel).MinSimilarity
	if sem.MinSimilarity != want {
		t.Errorf("floor is %v, want the model's %v", sem.MinSimilarity, want)
	}

	unmeasured := embed.NewFake(8)
	sem, err = SemanticFor(context.Background(), unmeasured, "duplicate imports", SemanticOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if sem.MinSimilarity != 0 {
		t.Errorf("an unmeasured model got a floor of %v, want none", sem.MinSimilarity)
	}

	none := 0.0
	sem, err = SemanticFor(context.Background(), calibrated, "duplicate imports",
		SemanticOptions{MinSimilarity: &none})
	if err != nil {
		t.Fatal(err)
	}
	if sem.MinSimilarity != 0 {
		t.Errorf("an explicit request for no floor produced %v", sem.MinSimilarity)
	}
}

func TestTheFloorExcludesWhatFallsBelowIt(t *testing.T) {
	// Geometry stated rather than discovered: the near entry sits at cos(0.2)
	// ≈ 0.98 to the query, the far one is orthogonal to it.
	s := embedStore(t)
	const dim = 8
	f := pin(t, dim)

	near := msg{id: "floor-near@example.com", subject: "Billing csv",
		body: "The nightly billing csv landed twice and both copies were loaded."}
	far := msg{id: "floor-far@example.com", subject: "Team lunch",
		body: "Booking the back room at the usual place for Friday, twelve of us."}
	put(t, s, near)
	put(t, s, far)
	pinText(t, f, SourceMail, near.subject, near.body, embed.Mix(dim, 0, 1, 0.2))
	pinText(t, f, SourceMail, far.subject, far.body, embed.Unit(dim, 1, 1))
	f.Texts["duplicate imports"] = embed.Unit(dim, 0, 1)
	backfill(t, s, f, BackfillOptions{})

	q := Query{Text: "duplicate imports", Semantic: &SemanticQuery{
		Vector: queryVector(t, f, "duplicate imports"), Model: f.Model(), Only: true}}
	hits, err := s.SearchEntries(q)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 {
		t.Fatalf("with no floor, search returned %d entries, want both", len(hits))
	}

	q.Semantic.MinSimilarity = 0.5
	hits, err = s.SearchEntries(q)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 {
		t.Fatalf("with a floor of 0.5, search returned %d entries, want only the near one", len(hits))
	}
	if hits[0].ExtID != "mail:"+near.id {
		t.Errorf("the surviving hit is %s, want the near entry", hits[0].ExtID)
	}
}
