package corpus

import (
	"strings"
	"testing"
	"time"
)

// Everything here is invented. This package indexes real personal
// correspondence, so committed fixtures use example.com addresses and content
// written for the test.

var (
	may  = time.Date(2025, 5, 10, 9, 0, 0, 0, time.UTC)
	june = time.Date(2025, 6, 10, 9, 0, 0, 0, time.UTC)
	july = time.Date(2025, 7, 10, 9, 0, 0, 0, time.UTC)
)

// searchPerson resolves a person through the same path ingest uses, so the
// stored identity values are normalised exactly as they will be in production.
func searchPerson(t *testing.T, s *Store, name, email string) int64 {
	t.Helper()
	id, err := Resolve(s, KindEmail, email, name)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// msg is a mail entry described the way a caller would: the parent is named by
// Message-ID, and ResolveParents links it, which is the only route Put offers
// into the reply graph.
type msg struct {
	id        string // becomes both ext_id and Message-ID
	parent    string // Message-ID of the parent, or ""
	subject   string
	body      string
	ts        time.Time
	person    int64
	to        string // a To: header, recorded as participants
	container string
	atts      []Attachment
}

func put(t *testing.T, s *Store, m msg) int64 {
	t.Helper()
	if m.ts.IsZero() {
		m.ts = june
	}
	e := Entry{
		Source: SourceMail, ExtID: "mail:" + m.id, TS: m.ts,
		PersonID: m.person, Container: m.container, ParentRef: m.parent,
		Subject: m.subject, BodyText: m.body,
	}
	r, err := s.Put(e, &Mail{MessageID: m.id, From: "someone@example.com", To: m.to}, m.atts)
	if err != nil {
		t.Fatalf("put %s: %v", m.id, err)
	}
	if m.person != 0 {
		if err := Participate(s, r.ID, m.person, RoleFrom); err != nil {
			t.Fatal(err)
		}
	}
	if m.to != "" {
		if _, err := RecordHeader(s, r.ID, RoleTo, m.to); err != nil {
			t.Fatal(err)
		}
	}
	return r.ID
}

// slackMsg adds a Slack entry plus its slack_detail row. Put has no Slack
// counterpart to its *Mail argument, so the detail row goes in by hand.
func slackMsg(t *testing.T, s *Store, ext, body string, ts time.Time, isBot bool, subtype string) int64 {
	t.Helper()
	r, err := s.Put(Entry{
		Source: SourceSlack, ExtID: ext, TS: ts, Container: "C123", BodyText: body,
	}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	bot := 0
	if isBot {
		bot = 1
	}
	if _, err := s.DB().Exec(`
		insert into slack_detail (entry_id, channel_id, ts, subtype, is_bot)
		values (?,?,?,?,?)`, r.ID, "C123", "1.0", nullStr(subtype), bot); err != nil {
		t.Fatal(err)
	}
	return r.ID
}

func ids(hits []EntryHit) []string {
	out := make([]string, len(hits))
	for i, h := range hits {
		out[i] = h.ExtID
	}
	return out
}

// The stemmer is the thing that most often makes a search test lie, so assert
// against the terms FTS5 actually indexed rather than against a match count:
// "levy" is stored as "levi", so a later body containing "levies" would still
// satisfy MATCH 'levy' and a count-based assertion would prove nothing.
func TestPorterIndexStoresStemsNotWords(t *testing.T) {
	s := open(t)
	put(t, s, msg{id: "<a@example.com>", body: "Confirming the levy was applied"})

	if _, err := s.DB().Exec(
		`create virtual table vocab using fts5vocab(entries_fts, 'row')`); err != nil {
		t.Fatalf("fts5vocab: %v", err)
	}
	rows, err := s.DB().Query(`select term from vocab`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	got := map[string]bool{}
	for rows.Next() {
		var term string
		if err := rows.Scan(&term); err != nil {
			t.Fatal(err)
		}
		got[term] = true
	}
	for _, want := range []string{"confirm", "levi", "appli"} {
		if !got[want] {
			t.Errorf("indexed terms missing stem %q; got %v", want, got)
		}
	}
	for _, unwanted := range []string{"confirming", "levy", "applied"} {
		if got[unwanted] {
			t.Errorf("indexed terms contain unstemmed %q", unwanted)
		}
	}
}

func TestStructuralFiltersAreAndedWithTheTextQuery(t *testing.T) {
	s := open(t)
	alice := searchPerson(t, s, "Alice", "alice@example.com")
	bob := searchPerson(t, s, "Bob", "bob@example.com")

	// The word is the same in all four; only the structure differs. That is the
	// point: "everything Alice sent about rebates after June" must not be
	// answerable by text alone.
	put(t, s, msg{id: "<a1@example.com>", body: "the rebate schedule", ts: may, person: alice})
	put(t, s, msg{id: "<a2@example.com>", body: "the rebate schedule", ts: july, person: alice})
	put(t, s, msg{id: "<b1@example.com>", body: "the rebate schedule", ts: july, person: bob})
	put(t, s, msg{id: "<b2@example.com>", body: "unrelated wording", ts: july, person: bob})

	hits, err := s.SearchEntries(Query{
		Text:   "rebate",
		Since:  june,
		People: []string{"ALICE@EXAMPLE.COM"}, // case-insensitive on purpose
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].ExtID != "mail:<a2@example.com>" {
		t.Fatalf("date+person+text: got %v, want just mail:<a2@example.com>", ids(hits))
	}

	// A display name must work as well as an address: callers should not have to
	// know which identity the corpus happened to record.
	hits, err = s.SearchEntries(Query{Text: "rebate", Since: june, People: []string{"alice"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 {
		t.Fatalf("person by display name: got %v", ids(hits))
	}

	// Until is half-open, so a boundary timestamp belongs to the later window.
	hits, err = s.SearchEntries(Query{Text: "rebate", Until: july})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].ExtID != "mail:<a1@example.com>" {
		t.Fatalf("until is half-open: got %v, want only the May entry", ids(hits))
	}
}

// The corpus stores identities normalised — emails lowercased, Slack uids
// uppercased and stripped of <@ >. A caller typing what they see in a header
// must still land on the right person, and must not have to say which kind of
// identity they typed.
func TestPeopleFilterAcceptsAnyIdentityForm(t *testing.T) {
	s := open(t)
	alice := searchPerson(t, s, "Alice Smith", "alice@example.com")
	if err := AddAlias(s, alice, KindSlackUID, "U04ABC", "test"); err != nil {
		t.Fatal(err)
	}
	put(t, s, msg{id: "<a@example.com>", body: "the rebate letter", person: alice})
	put(t, s, msg{id: "<b@example.com>", body: "the rebate letter",
		person: searchPerson(t, s, "Bob", "bob@example.com")})

	for _, name := range []string{
		"alice@example.com",
		"ALICE@EXAMPLE.COM",
		"<alice@example.com>", // as it appears in a raw header
		"U04ABC",
		"<@u04abc>", // as it appears in Slack text
		"Alice Smith",
		"alice smith",
	} {
		hits, err := s.SearchEntries(Query{Text: "rebate", People: []string{name}})
		if err != nil {
			t.Fatalf("People=%q: %v", name, err)
		}
		if len(hits) != 1 || hits[0].ExtID != "mail:<a@example.com>" {
			t.Errorf("People=%q matched %v, want just Alice's entry", name, ids(hits))
		}
	}

	// A name nobody answers to must match nothing rather than everything — the
	// dangerous failure here is a filter that quietly evaporates.
	for _, name := range []string{"nobody@example.com", "   "} {
		hits, err := s.SearchEntries(Query{Text: "rebate", People: []string{name}})
		if err != nil {
			t.Fatalf("People=%q: %v", name, err)
		}
		if len(hits) != 0 {
			t.Errorf("People=%q matched %v, want nothing", name, ids(hits))
		}
	}

	// PersonIDs is the same filter for a caller that already resolved the person.
	hits, err := s.SearchEntries(Query{Text: "rebate", PersonIDs: []int64{alice}})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].ExtID != "mail:<a@example.com>" {
		t.Fatalf("PersonIDs: got %v", ids(hits))
	}
}

// People and Involving answer different questions, and a search layer that
// conflated them would hide every thread someone was merely cc'd on.
func TestInvolvingMatchesRecipientsAndPeopleDoesNot(t *testing.T) {
	s := open(t)
	alice := searchPerson(t, s, "Alice", "alice@example.com")
	// Carol never sends anything; she only ever appears in a To: header.
	put(t, s, msg{id: "<a@example.com>", body: "the rebate letter",
		person: alice, to: "Carol <carol@example.com>"})
	put(t, s, msg{id: "<b@example.com>", body: "the rebate letter", person: alice})

	sent, err := s.SearchEntries(Query{Text: "rebate", People: []string{"carol@example.com"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(sent) != 0 {
		t.Errorf("People matched %v; Carol authored nothing", ids(sent))
	}

	on, err := s.SearchEntries(Query{Text: "rebate", Involving: []string{"carol@example.com"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(on) != 1 || on[0].ExtID != "mail:<a@example.com>" {
		t.Fatalf("Involving matched %v, want the entry Carol was addressed on", ids(on))
	}

	// Alice is involved in both, as the author.
	on, err = s.SearchEntries(Query{Text: "rebate", Involving: []string{"alice@example.com"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(on) != 2 {
		t.Fatalf("Involving alice matched %v, want both", ids(on))
	}

	// Roles narrow it: Carol is a recipient, never a sender.
	on, err = s.SearchEntries(Query{Text: "rebate",
		Involving: []string{"carol@example.com"}, InvolvingRoles: []string{RoleFrom}})
	if err != nil {
		t.Fatal(err)
	}
	if len(on) != 0 {
		t.Errorf("InvolvingRoles=[from] matched %v for a pure recipient", ids(on))
	}
	on, err = s.SearchEntries(Query{Text: "rebate",
		Involving: []string{"carol@example.com"}, InvolvingRoles: []string{RoleTo, RoleCc}})
	if err != nil {
		t.Fatal(err)
	}
	if len(on) != 1 {
		t.Errorf("InvolvingRoles=[to,cc] matched %v, want one", ids(on))
	}
}

func TestStructuralOnlyQueryNeedsNoText(t *testing.T) {
	s := open(t)
	put(t, s, msg{id: "<old@example.com>", body: "first", ts: may})
	put(t, s, msg{id: "<new@example.com>", body: "second", ts: july})

	hits, err := s.SearchEntries(Query{Since: june})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].ExtID != "mail:<new@example.com>" {
		t.Fatalf("filter with no text: got %v", ids(hits))
	}
	if hits[0].Snippet != "" {
		t.Errorf("structural-only hit has a snippet %q; nothing matched to highlight", hits[0].Snippet)
	}
}

func TestHasAttachmentFilterBothWays(t *testing.T) {
	s := open(t)
	put(t, s, msg{id: "<with@example.com>", body: "the rebate statement",
		atts: []Attachment{{Name: "statement.pdf"}}})
	put(t, s, msg{id: "<without@example.com>", body: "the rebate statement"})

	yes, no := true, false
	hits, err := s.SearchEntries(Query{Text: "rebate", HasAttachment: &yes})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].ExtID != "mail:<with@example.com>" {
		t.Fatalf("has-attachment: got %v", ids(hits))
	}
	hits, err = s.SearchEntries(Query{Text: "rebate", HasAttachment: &no})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].ExtID != "mail:<without@example.com>" {
		t.Fatalf("no-attachment: got %v", ids(hits))
	}
}

// The trigram index earns its place only on substrings the word tokenizers
// cannot produce. 'ZZQ-4417' is one porter token, so a search for a fragment of
// it can only be answered by trigram.
func TestIdentifierFragmentIsFoundOnlyByTheTrigramIndex(t *testing.T) {
	s := open(t)
	put(t, s, msg{id: "<inv@example.com>", subject: "Statement",
		body: "Please see invoice ZZQ-4417 attached"})

	hits, err := s.SearchEntries(Query{Text: "Q-4417"})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 {
		t.Fatalf("identifier fragment: got %v, want one hit", ids(hits))
	}
	if hits[0].IdentRank == 0 {
		t.Error("the trigram index did not rank the hit that only it can find")
	}
	if hits[0].ProseRank != 0 {
		t.Errorf("the porter index claims to have found %q; it cannot", "Q-4417")
	}
	if hits[0].Score <= 0 {
		t.Errorf("score %v, want the surviving index's votes to stand alone", hits[0].Score)
	}

	// The mirror case: a prose word the trigram side is never asked about.
	hits, err = s.SearchEntries(Query{Text: "attaching"}) // stems to "attach"
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].ProseRank == 0 || hits[0].IdentRank != 0 {
		t.Fatalf("prose-only query: got %+v", hits)
	}
}

// Fusion has to actually add up: an entry both indexes agree on must outrank an
// entry only one of them found.
func TestReciprocalRankFusionRewardsAgreement(t *testing.T) {
	s := open(t)
	// "0000035363EA0FE" is a whole porter token in `both`, so porter can match it.
	put(t, s, msg{id: "<both@example.com>", body: "ICP 0000035363EA0FE was disputed"})
	// Glued into a longer run, so porter tokenises it as something else entirely
	// and only trigram can see the identifier inside.
	put(t, s, msg{id: "<trigramonly@example.com>", body: "ref XX0000035363EA0FEYY logged"})

	hits, err := s.SearchEntries(Query{Text: "0000035363EA0FE"})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 {
		t.Fatalf("got %v, want both entries", ids(hits))
	}
	if hits[0].ExtID != "mail:<both@example.com>" {
		t.Fatalf("ranked %v first; the entry both indexes found must lead", hits[0].ExtID)
	}
	if hits[0].ProseRank == 0 || hits[0].IdentRank == 0 {
		t.Errorf("expected the leader to be found by both indexes, got %+v", hits[0])
	}
	if hits[1].ProseRank != 0 || hits[1].IdentRank == 0 {
		t.Errorf("expected the runner-up to be trigram-only, got %+v", hits[1])
	}
	if !(hits[0].Score > hits[1].Score) {
		t.Errorf("scores %v then %v: two votes must beat one", hits[0].Score, hits[1].Score)
	}
}

func TestSnippetShowsWhyTheEntryMatched(t *testing.T) {
	s := open(t)
	put(t, s, msg{id: "<a@example.com>", subject: "Quarterly statement",
		body: "We have confirmed the rebate and will forward the paperwork shortly."})

	hits, err := s.SearchEntries(Query{Text: "rebate"})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 {
		t.Fatalf("got %v", ids(hits))
	}
	if !strings.Contains(hits[0].Snippet, snippetOpen+"rebate"+snippetClose) {
		t.Errorf("snippet %q does not mark the matched term", hits[0].Snippet)
	}
}

// The headline requirement: rank threads, not sentences. Two chains mention the
// same term; the one where it is the topic must win, and the loser must still
// appear.
func TestChainsRankByAggregateRelevance(t *testing.T) {
	s := open(t)
	// Chain A: the term recurs. Note the deliberately varied word forms — porter
	// folds "rebate" and "rebates" together, which is the behaviour we want.
	put(t, s, msg{id: "<a1@example.com>", subject: "Rebate reconciliation", body: "opening the rebate question", ts: may})
	put(t, s, msg{id: "<a2@example.com>", parent: "<a1@example.com>", body: "the rebates were understated", ts: june})
	put(t, s, msg{id: "<a3@example.com>", parent: "<a2@example.com>", body: "agreed, rebate corrected", ts: july})
	// Chain B: one passing mention inside an otherwise unrelated trail.
	put(t, s, msg{id: "<b1@example.com>", subject: "Meter swap", body: "scheduling the swap", ts: may})
	put(t, s, msg{id: "<b2@example.com>", parent: "<b1@example.com>", body: "also a rebate note", ts: june})
	if _, err := s.ResolveParents(); err != nil {
		t.Fatal(err)
	}

	chains, err := s.SearchChains(Query{Text: "rebate"})
	if err != nil {
		t.Fatal(err)
	}
	if len(chains) != 2 {
		t.Fatalf("got %d chains, want 2: %+v", len(chains), chains)
	}
	if chains[0].RootExtID != "mail:<a1@example.com>" {
		t.Fatalf("chain order: got %q first, want the trail the term is about",
			chains[0].RootExtID)
	}
	if chains[0].Score <= chains[1].Score {
		t.Errorf("scores %v then %v", chains[0].Score, chains[1].Score)
	}

	// Matched vs Entries is the "is this thread about it" ratio, so both must be
	// reported, and Entries must count the whole chain rather than the hits.
	if chains[0].Matched != 3 || chains[0].Entries != 3 {
		t.Errorf("chain A: matched %d of %d, want 3 of 3", chains[0].Matched, chains[0].Entries)
	}
	if chains[1].Matched != 1 || chains[1].Entries != 2 {
		t.Errorf("chain B: matched %d of %d, want 1 of 2", chains[1].Matched, chains[1].Entries)
	}
	if !chains[0].First.Equal(may) || !chains[0].Last.Equal(july) {
		t.Errorf("chain A span %v..%v, want the whole chain's span", chains[0].First, chains[0].Last)
	}
	if chains[0].Subject != "Rebate reconciliation" {
		t.Errorf("chain subject %q, want the root's", chains[0].Subject)
	}
	if len(chains[0].Best) == 0 || chains[0].Best[0].Snippet == "" {
		t.Errorf("chain must carry its best-matching entries with excerpts: %+v", chains[0].Best)
	}

	// PerChain caps the evidence attached, not the chain's score.
	capped, err := s.SearchChains(Query{Text: "rebate", PerChain: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(capped[0].Best) != 1 {
		t.Errorf("PerChain=1 attached %d entries", len(capped[0].Best))
	}
	if capped[0].Score != chains[0].Score {
		t.Errorf("PerChain changed the score: %v vs %v", capped[0].Score, chains[0].Score)
	}
}

// Why the reply graph and not `container`. Both halves of this test would fail
// under a group-by-container implementation.
func TestChainsFollowTheReplyGraphNotTheContainer(t *testing.T) {
	s := open(t)
	// One trail whose members were filed under different containers, plus an
	// original recovered from quoted text with no container at all. They are one
	// conversation because the reply edges say so.
	put(t, s, msg{id: "<r1@example.com>", subject: "Access request", body: "the levy dispute begins", container: ""})
	put(t, s, msg{id: "<r2@example.com>", parent: "<r1@example.com>", body: "levy dispute continues", container: "thread-1"})
	put(t, s, msg{id: "<r3@example.com>", parent: "<r2@example.com>", body: "levy dispute resolved", container: "thread-2"})
	// Two unrelated messages that Gmail happened to file together, with no reply
	// edge between them. Container would merge these; the reply graph must not.
	put(t, s, msg{id: "<u1@example.com>", body: "separate levy enquiry", container: "thread-9"})
	put(t, s, msg{id: "<u2@example.com>", body: "another levy enquiry", container: "thread-9"})
	if _, err := s.ResolveParents(); err != nil {
		t.Fatal(err)
	}

	chains, err := s.SearchChains(Query{Text: "levy"})
	if err != nil {
		t.Fatal(err)
	}
	if len(chains) != 3 {
		t.Fatalf("got %d chains, want 3 (one linked trail + two unlinked singletons)", len(chains))
	}
	var linked *ChainHit
	for i := range chains {
		if chains[i].RootExtID == "mail:<r1@example.com>" {
			linked = &chains[i]
		}
	}
	if linked == nil {
		t.Fatalf("the linked trail did not surface: %+v", chains)
	}
	if linked.Matched != 3 || linked.Entries != 3 {
		t.Errorf("cross-container trail: matched %d of %d, want 3 of 3", linked.Matched, linked.Entries)
	}
	for _, c := range chains {
		if c.RootExtID != "mail:<r1@example.com>" && c.Entries != 1 {
			t.Errorf("chain %s has %d entries; shared-container entries with no reply "+
				"edge must not be merged", c.RootExtID, c.Entries)
		}
	}

	// A dangling parent_ref makes an entry its own root — the parent really is
	// outside the mailbox, and inventing a grouping would hide that.
	put(t, s, msg{id: "<orphan@example.com>", parent: "<never-received@example.net>",
		body: "levy dispute, forwarded on", container: "thread-1"})
	if _, err := s.ResolveParents(); err != nil {
		t.Fatal(err)
	}
	chains, err = s.SearchChains(Query{Text: "levy"})
	if err != nil {
		t.Fatal(err)
	}
	if len(chains) != 4 {
		t.Fatalf("got %d chains, want 4: an unresolved parent is its own root", len(chains))
	}
}

func TestNoiseIsExcludedByDefaultAndIncludableByFlag(t *testing.T) {
	s := open(t)
	slackMsg(t, s, "slack:C123:1", "chasing the rebate again", june, false, "")
	slackMsg(t, s, "slack:C123:2", "rebate report generated automatically", june, true, "")
	slackMsg(t, s, "slack:C123:3", "rebate channel joined", june, false, "channel_join")
	// A mail entry, to prove the Slack-only predicates do not take mail with
	// them: slack_detail is NULL-joined here, and `not (NULL)` is not false.
	put(t, s, msg{id: "<m@example.com>", body: "the rebate letter"})

	hits, err := s.SearchEntries(Query{Text: "rebate"})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 {
		t.Fatalf("default search: got %v, want the human Slack message and the mail", ids(hits))
	}
	seen := map[string]bool{}
	for _, h := range hits {
		seen[h.ExtID] = true
	}
	if !seen["slack:C123:1"] || !seen["mail:<m@example.com>"] {
		t.Fatalf("default search dropped a non-noise entry: %v", ids(hits))
	}

	all, err := s.SearchEntries(Query{Text: "rebate", IncludeNoise: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 4 {
		t.Fatalf("IncludeNoise: got %v, want all four", ids(all))
	}

	// The filter is data, so a caller can widen it — this is the seam a mail
	// rule will use once there is something on mail_detail to key off.
	custom := NoiseFilter{Predicates: []string{`md.from_addr = 'someone@example.com'`}}
	hits, err = s.SearchEntries(Query{Text: "rebate", Noise: &custom})
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range hits {
		if h.ExtID == "mail:<m@example.com>" {
			t.Error("a custom mail-side noise predicate was not applied")
		}
	}
	if len(hits) != 3 {
		t.Fatalf("custom filter: got %v, want the three Slack entries", ids(hits))
	}
}

func TestSourceAndContainerFilters(t *testing.T) {
	s := open(t)
	slackMsg(t, s, "slack:C123:1", "the rebate thread", june, false, "")
	put(t, s, msg{id: "<m@example.com>", body: "the rebate letter", container: "thread-1"})

	hits, err := s.SearchEntries(Query{Text: "rebate", Sources: []string{SourceSlack}})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].Source != SourceSlack {
		t.Fatalf("source filter: got %v", ids(hits))
	}
	hits, err = s.SearchEntries(Query{Text: "rebate", Containers: []string{"thread-1"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].ExtID != "mail:<m@example.com>" {
		t.Fatalf("container filter: got %v", ids(hits))
	}
}

// The cast is everyone involved, not the authors of the hits: a recipient who
// only ever appears in To: is a participant too, and must count.
func TestChainPeopleCountsTheWholeCast(t *testing.T) {
	s := open(t)
	alice := searchPerson(t, s, "Alice Example", "alice@example.com")
	bob := searchPerson(t, s, "Bob Example", "bob@example.com")
	cara := searchPerson(t, s, "Cara Example", "cara@example.com")

	// One chain: Alice writes, Bob is cc'd around, Cara is the lone voice in
	// the thread. All three are the cast even though the query only hit Alice.
	put(t, s, msg{id: "<a1@example.com>", body: "the rebate letter", ts: may,
		person: alice, to: "Bob Example <bob@example.com>"})
	put(t, s, msg{id: "<a2@example.com>", parent: "<a1@example.com>", body: "the rebate reply", ts: june,
		person: cara})
	// A second, unrelated chain with one participant.
	put(t, s, msg{id: "<b1@example.com>", body: "the rebate schedule", ts: july, person: bob})
	if _, err := s.ResolveParents(); err != nil {
		t.Fatal(err)
	}

	chains, err := s.SearchChains(Query{Text: "rebate"})
	if err != nil {
		t.Fatal(err)
	}
	if len(chains) != 2 {
		t.Fatalf("got %d chains, want 2", len(chains))
	}
	if chains[0].People != 3 {
		t.Errorf("chain A: people = %d, want 3 (Alice, Bob in To:, Cara)", chains[0].People)
	}
	if chains[1].People != 1 {
		t.Errorf("chain B: people = %d, want 1 (Bob only)", chains[1].People)
	}
}

func TestLimitAppliesToChainsNotToTheCandidatePool(t *testing.T) {
	s := open(t)
	// Two chains. A narrow candidate pool must not be mistaken for a narrow
	// answer: chain evidence is spread across entries, so Limit and
	// CandidateLimit are separate knobs.
	put(t, s, msg{id: "<a1@example.com>", body: "rebate one", ts: may})
	put(t, s, msg{id: "<a2@example.com>", parent: "<a1@example.com>", body: "rebate two", ts: june})
	put(t, s, msg{id: "<b1@example.com>", body: "rebate three", ts: july})
	if _, err := s.ResolveParents(); err != nil {
		t.Fatal(err)
	}

	chains, err := s.SearchChains(Query{Text: "rebate", Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(chains) != 1 {
		t.Fatalf("Limit=1 returned %d chains", len(chains))
	}
	hits, err := s.SearchEntries(Query{Text: "rebate", Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 {
		t.Fatalf("Limit=2 returned %d entries", len(hits))
	}
}

func TestEmptyResultsAreEmptyNotAnError(t *testing.T) {
	s := open(t)
	put(t, s, msg{id: "<a@example.com>", body: "nothing of interest"})

	hits, err := s.SearchEntries(Query{Text: "rebate"})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 0 {
		t.Fatalf("got %v", ids(hits))
	}
	chains, err := s.SearchChains(Query{Text: "rebate"})
	if err != nil {
		t.Fatal(err)
	}
	if len(chains) != 0 {
		t.Fatalf("got %d chains", len(chains))
	}
}

// FTS5 parses bare AND / NEAR / * as operators, so a user typing them must not
// produce a syntax error from the depths of the query layer.
func TestOperatorLookingTextIsTreatedAsWords(t *testing.T) {
	s := open(t)
	put(t, s, msg{id: "<a@example.com>", body: "the AND clause and a NEAR miss"})

	for _, text := range []string{"AND", "NEAR", "*", "OR NOT", `he said "quoted"`, "^start"} {
		if _, err := s.SearchEntries(Query{Text: text}); err != nil {
			t.Errorf("Text=%q: %v", text, err)
		}
	}
}

func TestQueryTermsSplitting(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"   ", nil},
		{"rebate schedule", []string{"rebate", "schedule"}},
		// trailing punctuation must not become part of an identifier
		{"invoice ZZQ-4417.", []string{"invoice", "ZZQ-4417"}},
		{"(rebate),", []string{"rebate"}},
		// a quoted run is one phrase
		{`"meter swap" rebate`, []string{"meter swap", "rebate"}},
		// an unterminated quote is still usable
		{`"meter swap`, []string{"meter swap"}},
	} {
		got := QueryTerms(tc.in)
		if len(got) != len(tc.want) {
			t.Errorf("QueryTerms(%q) = %v, want %v", tc.in, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("QueryTerms(%q) = %v, want %v", tc.in, got, tc.want)
				break
			}
		}
	}
}

func TestLooksLikeIdentifier(t *testing.T) {
	for _, s := range []string{"CINV-00066864", "0000035363EA0FE", "ZZQ-4417", "re-send", "2025"} {
		if !LooksLikeIdentifier(s) {
			t.Errorf("LooksLikeIdentifier(%q) = false", s)
		}
	}
	for _, s := range []string{"rebate", "meter", "", "-", "--", "-lead", "trail-"} {
		if LooksLikeIdentifier(s) {
			t.Errorf("LooksLikeIdentifier(%q) = true", s)
		}
	}
}

func TestMatchExpressionsRouteTermsToTheRightIndexes(t *testing.T) {
	// Every term is offered to the porter index; only identifier-shaped ones go
	// to trigram, and only when they are long enough for a trigram to exist.
	prose, ident := MatchExpressions("rebate ZZQ-4417")
	if prose != `"rebate" AND "ZZQ-4417"` {
		t.Errorf("prose = %q", prose)
	}
	if ident != `"ZZQ-4417"` {
		t.Errorf("ident = %q", ident)
	}

	if prose, ident = MatchExpressions("rebate schedule"); ident != "" {
		t.Errorf("ordinary prose reached the trigram index: %q", ident)
	} else if prose == "" {
		t.Error("prose expression is empty")
	}

	// Shorter than a trigram: trigram cannot answer, so do not ask it.
	if _, ident = MatchExpressions("A1"); ident != "" {
		t.Errorf("two-character identifier reached the trigram index: %q", ident)
	}

	if p, i := MatchExpressions("   "); p != "" || i != "" {
		t.Errorf("empty text produced %q / %q", p, i)
	}

	// A stray quote is a phrase delimiter, so it never reaches quoteFTS as data.
	if p, _ := MatchExpressions(`a"b`); p != `"a" AND "b"` {
		t.Errorf("stray quote: %q", p)
	}
	// quoteFTS still doubles quotes, because it is the only thing standing
	// between a term and the FTS5 expression parser.
	if got := quoteFTS(`a"b`); got != `"a""b"` {
		t.Errorf("quoteFTS = %q", got)
	}
}

// Slack entries carry no subject by design, so a chain rooted in one had nothing
// to show but its ext_id — "slack:C08JDEG2C83:1784618363.054409" tells a reader
// nothing about which conversation matched.
func TestSlackChainsAreLabelledByConversation(t *testing.T) {
	s := open(t)

	put := func(ext, channel, chanName, ts, body string) {
		e := Entry{
			Source: SourceSlack, ExtID: ext, Kind: "message",
			TS: time.Unix(1_700_000_000, 0), Container: channel, BodyText: body,
		}
		if _, err := s.PutSlack(e, Slack{
			ChannelID: channel, ChannelName: chanName, TS: ts,
		}, nil); err != nil {
			t.Fatal(err)
		}
	}
	put("slack:C1:1.1", "C1", "rate-reviews", "1.1", "the unbundled tariff question")
	put("slack:D1:2.1", "D1", "@U0ABC", "2.1", "the unbundled tariff question")

	// The DM's stored name is the other party's raw uid; it should resolve.
	who, err := Resolve(s, "slack_uid", "U0ABC", "Bo Vantel")
	if err != nil {
		t.Fatal(err)
	}
	if who == 0 {
		t.Fatal("no person created")
	}

	hits, err := s.SearchChains(Query{Text: "unbundled", Limit: 10, PerChain: 3})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, h := range hits {
		got[h.Subject] = true
	}
	if !got["#rate-reviews"] {
		t.Errorf("channel chain not labelled #rate-reviews: %v", keysOf(got))
	}
	if !got["@Bo Vantel"] {
		t.Errorf("DM chain not labelled by the person: %v", keysOf(got))
	}
	// And never a bare ext_id where a label exists.
	for k := range got {
		if strings.HasPrefix(k, "slack:") {
			t.Errorf("chain still labelled by ext_id: %q", k)
		}
	}
}

func keysOf(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
