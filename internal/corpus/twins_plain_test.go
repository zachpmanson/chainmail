package corpus

import (
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/html"
)

// These names, addresses and message contents are invented.
const mentionPlain = `+Add @Ada Example <ada@example.test> @Bo Example <bo@example.test> Please review the revised inventory summary before the warehouse planning meeting. The numbered cartons are separate while the shipment proposal groups them together. Can everyone confirm the correct arrangement before tomorrow? Thanks, Rowan`
const mentionHTML = `<div dir="ltr">+Add <a class="gmail_plusreply" id="mention-a" href="mailto:ada@example.test" tabindex="-1">@Ada Example</a> <a href="mailto:bo@example.test">@Bo Example</a> Please review the revised inventory summary before the warehouse planning meeting. The numbered cartons are separate while the shipment proposal groups them together.<br><br>Can everyone confirm the correct arrangement before tomorrow?<div>Thanks, Rowan</div></div>`
const mentionHistory = `<div class="gmail_quote gmail_quote_container"><div class="gmail_attr">On Monday, another sender wrote:</div><blockquote>Previous inventory discussion and a signature.<img src="https://images.example.test/one.png"><img src="https://images.example.test/two.png"></blockquote></div>`

func mentionCopies() (twinCopy, twinCopy) {
	a := twinCopy{id: 1, ext: "mail:<mention@example.test>", wall: time.Date(2026, 1, 5, 3, 0, 0, 0, time.UTC), container: "inventory", bodyHTML: mentionHTML + mentionHistory}
	b := twinCopy{id: 2, ext: "quote:invented", quoted: true, wall: a.wall.Add(8 * time.Hour), container: a.container}
	reduceTwinBody(&a, mentionPlain)
	reduceTwinBody(&b, mentionPlain)
	return a, b
}

func TestPlainTwinMentionFallback(t *testing.T) {
	a, b := mentionCopies()
	primaryA, primaryB := a, b
	primaryA.plain, primaryB.plain = "", ""
	if _, ok := twinnable(primaryA, primaryB); ok {
		t.Fatal("fixture must fail primary identity before fallback")
	}
	if len(a.words) == len(b.words) {
		t.Fatal("fixture must retain distinct primary representations")
	}
	for _, pair := range [][2]twinCopy{{a, b}, {b, a}} {
		if why, ok := twinnable(pair[0], pair[1]); !ok {
			t.Fatalf("exact own plaintext should match: %s", why)
		}
	}
	if why, inline := annotates(a, b); why != "" || inline {
		t.Fatalf("unmodified quote classified as annotated: %s", why)
	}
	groups, _ := groupTwins([]twinCopy{a, b})
	if len(groups) != 1 {
		t.Fatalf("groups=%d", len(groups))
	}
	result, kept, why, ok := resolveTwins(groups[0])
	if !ok || len(kept) != 0 || len(result.Drop) != 1 || result.Keep != a.id {
		t.Fatalf("resolution=%+v kept=%+v why=%s", result, kept, why)
	}
	if !strings.Contains(a.bodyHTML, "one.png") {
		t.Fatal("source history changed")
	}
}

func TestPlainTwinProofRefusals(t *testing.T) {
	cases := map[string]string{
		"own sentence":          mentionHTML + `<div>Separate instructions only in HTML.</div>` + mentionHistory,
		"own image":             mentionHTML + `<img src="https://images.example.test/own.png">`,
		"script":                mentionHTML + `<script>different()</script>`,
		"style":                 mentionHTML + `<style>a{display:none}</style>`,
		"comment":               mentionHTML + `<!-- uncertain -->`,
		"unknown element":       strings.ReplaceAll(mentionHTML, "<div>", "<section>"),
		"arbitrary blockquote":  mentionHTML + `<blockquote>Different own content</blockquote>`,
		"hidden attribute":      strings.Replace(mentionHTML, `dir="ltr"`, `dir="ltr" hidden`, 1),
		"event attribute":       strings.Replace(mentionHTML, `dir="ltr"`, `dir="ltr" onclick="different()"`, 1),
		"style attribute":       strings.Replace(mentionHTML, `dir="ltr"`, `style="display:none"`, 1),
		"duplicate href":        strings.Replace(mentionHTML, `href="mailto:ada@example.test"`, `href="mailto:ada@example.test" href="mailto:other@example.test"`, 1),
		"duplicate quote class": mentionHTML + `<div class="gmail_quote" class="other">Different</div>`,
		"wrong address":         strings.Replace(mentionHTML, "mailto:ada@example.test", "mailto:other@example.test", 1),
		"changed address case":  strings.Replace(mentionHTML, "mailto:ada@example.test", "mailto:Ada@example.test", 1),
		"mailto query":          strings.Replace(mentionHTML, "mailto:ada@example.test", "mailto:ada@example.test?subject=Other", 1),
		"empty mailto query":    strings.Replace(mentionHTML, "mailto:ada@example.test", "mailto:ada@example.test?", 1),
		"mailto fragment":       strings.Replace(mentionHTML, "mailto:ada@example.test", "mailto:ada@example.test#other", 1),
		"mailto escaped":        strings.Replace(mentionHTML, "mailto:ada@example.test", "mailto:ada%40example.test", 1),
		"mailto list":           strings.Replace(mentionHTML, "mailto:ada@example.test", "mailto:ada@example.test,other@example.test", 1),
		"general link":          strings.Replace(mentionHTML, "mailto:ada@example.test", "https://other.example.test/secret", 1),
		"unsafe link":           strings.Replace(mentionHTML, "mailto:ada@example.test", "javascript:different()", 1),
		"no href":               strings.Replace(mentionHTML, `href="mailto:ada@example.test"`, "", 1),
		"missing anchor close":  strings.Replace(mentionHTML, "</a>", "", 1),
		"missing div close":     strings.TrimSuffix(mentionHTML, "</div>"),
		"misnested anchor":      strings.Replace(mentionHTML, "@Ada Example", "<div>@Ada Example</div>", 1),
		"self closed anchor":    strings.Replace(mentionHTML, `tabindex="-1">`, `tabindex="-1"/>`, 1),
		"unclosed history":      mentionHTML + `<div class="gmail_quote"><img src="https://images.example.test/old.png">`,
		"invalid UTF8":          mentionHTML + "\xff",
		"oversized":             strings.Repeat(" ", twinPlainMaxBytes+1) + mentionHTML,
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			if got := plainTwinEvidence(mentionPlain, raw, false); got != "" {
				t.Fatal("unsupported own HTML gained fallback evidence")
			}
		})
	}
	if got := plainTwinEvidence(mentionPlain, mentionHTML+mentionHistory, false); got != plainTwinText(mentionPlain) {
		t.Fatal("recognized quoted history contaminated own proof")
	}
	for name, body := range map[string]string{"empty": "", "short": "+Add @Ada Example <ada@example.test> hello there", "punctuation": strings.Replace(mentionPlain, "tomorrow?", "tomorrow!", 1), "case": strings.Replace(mentionPlain, "Please", "please", 1), "own URL": mentionPlain + " https://different.example.test/path"} {
		t.Run(name, func(t *testing.T) {
			if plainTwinEvidence(body, mentionHTML, false) != "" {
				t.Fatal("nonmatching or short plaintext gained evidence")
			}
		})
	}
	if plainTwinEvidence(mentionPlain, mentionHTML, true) != "" {
		t.Fatal("stored recovered HTML was ignored")
	}
	if plainTwinEvidence(mentionPlain, "<div>"+mentionPlain+"</div>", false) != "" {
		t.Fatal("proof lacked an actual mention expansion")
	}
}

func TestPlainTwinHistoryClassWhitespace(t *testing.T) {
	for name, separator := range map[string]string{
		"space": " ", "tab": "\t", "LF": "\n", "FF": "\f", "CR": "\r",
	} {
		t.Run(name, func(t *testing.T) {
			raw := mentionHTML + `<div class="before` + separator + `gmail_quote` + separator + `after">History with an image.<img src="https://images.example.test/old.png"></div>`
			assertPlainTwinHistoryDecision(t, raw, true)
		})
	}
	for name, separator := range map[string]string{
		"NBSP": "&nbsp;", "vertical tab": "&#x0b;", "EM SPACE": "&#x2003;",
	} {
		t.Run(name, func(t *testing.T) {
			raw := mentionHTML + `<div class="gmail_quote` + separator + `not-history">Unique new own HTML instructions.</div>`
			assertPlainTwinHistoryDecision(t, raw, false)
		})
	}
}

func TestPlainTwinParsedHistoryBoundaries(t *testing.T) {
	const image = `<img src="https://images.example.test/new-own.png">`
	for name, suffix := range map[string]string{
		"select image":         `<div class="gmail_quote"><select><div></select></div>` + image + `</div>`,
		"foreign select image": `<div class="gmail_quote"><svg><foreignObject><select><div></select></foreignObject></svg></div>` + image + `</div>`,
		"table colgroup image": `<div class="gmail_quote"><table><colgroup><div></colgroup></table></div>` + image + `</div>`,
		"table caption image":  `<div class="gmail_quote"><table><caption><select><div></select></caption></table></div>` + image + `</div>`,
	} {
		t.Run(name, func(t *testing.T) {
			raw := mentionHTML + suffix
			assertPlainTwinOwnImage(t, raw)
			if _, ok := mentionPlainTokens(raw); !ok {
				t.Fatal("fixture must reproduce the raw history-depth acceptance")
			}
			assertPlainTwinHistoryDecision(t, raw, false)
		})
	}
	for name, own := range map[string]string{
		"prose":   `<div>Unique new own HTML instructions.</div>`,
		"unknown": `<section>Unique new own HTML instructions.</section>`,
	} {
		t.Run(name, func(t *testing.T) {
			raw := mentionHTML + `<div class="gmail_quote"><select><div></select></div>` + own + `</div>`
			if _, ok := mentionPlainTokens(raw); !ok {
				t.Fatal("fixture must reproduce the raw history-depth acceptance")
			}
			assertPlainTwinHistoryDecision(t, raw, false)
		})
	}
	// Ordinary history formatting is not part of the own-content grammar.
	richHistory := `<div class="gmail_quote"><table><tbody><tr><td><b>Previous discussion</b><img src="https://images.example.test/old.png"></td></tr></tbody></table></div>`
	assertPlainTwinHistoryDecision(t, mentionHTML+richHistory, true)
}

func assertPlainTwinOwnImage(t *testing.T, raw string) {
	t.Helper()
	doc, err := html.Parse(strings.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	found := 0
	var visit func(*html.Node)
	visit = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "img" {
			for _, attr := range n.Attr {
				if attr.Key != "src" || attr.Val != "https://images.example.test/new-own.png" {
					continue
				}
				found++
				for parent := n.Parent; parent != nil; parent = parent.Parent {
					if parent.Type != html.ElementNode || parent.Data != "div" {
						continue
					}
					for _, class := range parent.Attr {
						if class.Key == "class" && class.Val == "gmail_quote" {
							t.Fatal("fixture image is inside actual history, not own content")
						}
					}
				}
			}
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			visit(child)
		}
	}
	visit(doc)
	if found != 1 {
		t.Fatalf("own images=%d, want 1", found)
	}
}

func assertPlainTwinHistoryDecision(t *testing.T, raw string, want bool) {
	t.Helper()
	if got := plainTwinEvidence(mentionPlain, raw, false) != ""; got != want {
		t.Fatalf("HTML proof=%v, want %v", got, want)
	}
	a, b := mentionCopies()
	a.bodyHTML = raw
	reduceTwinBody(&a, mentionPlain)
	primaryA, primaryB := a, b
	primaryA.plain, primaryB.plain = "", ""
	if _, ok := twinnable(primaryA, primaryB); ok {
		t.Fatal("history fixture must fail primary identity")
	}
	for _, pair := range [][2]twinCopy{{a, b}, {b, a}} {
		if why, ok := twinnable(pair[0], pair[1]); ok != want {
			t.Fatalf("identity=%v, want %v: %s", ok, want, why)
		}
	}
	groups, _ := groupTwins([]twinCopy{a, b})
	if got := len(groups) != 0; got != want {
		t.Fatalf("repair group=%v, want %v", got, want)
	}

	s := open(t)
	author := person(t, s, "history@example.test", "History Example")
	mailbox, err := s.Put(Entry{Source: SourceMail, ExtID: a.ext, TS: a.wall, PersonID: author, Container: a.container, BodyText: mentionPlain, BodyHTML: raw}, &Mail{MessageID: "<mention@example.test>"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, ok, err := FindTwin(s, author, b.wall, mentionPlain, "", a.container)
	if err != nil || ok != want || (ok && got != mailbox.ID) {
		t.Fatalf("FindTwin id=%d ok=%v, want %v: %v", got, ok, want, err)
	}
	if _, _, err = s.PutQuoted(Entry{Source: SourceMail, ExtID: b.ext, TS: b.wall, PersonID: author, Container: b.container, BodyText: mentionPlain}); err != nil {
		t.Fatal(err)
	}
	removed := 0
	if want {
		removed = 1
	}
	for _, apply := range []bool{false, true} {
		plan, err := CollapseTwins(s, apply)
		if err != nil || plan.Removed != removed {
			t.Fatalf("repair apply=%v removed=%d, want %d: %v", apply, plan.Removed, removed, err)
		}
	}
	var remaining int
	if err = s.db.QueryRow(`select count(*) from entries`).Scan(&remaining); err != nil || remaining != 2-removed {
		t.Fatalf("remaining=%d, want %d: %v", remaining, 2-removed, err)
	}
	var stored string
	if err = s.db.QueryRow(`select body_html from entries where id=?`, mailbox.ID).Scan(&stored); err != nil || stored != raw {
		t.Fatal("stored original HTML changed", err)
	}
}

func TestPlainTwinPairGuards(t *testing.T) {
	for name, change := range map[string]func(*twinCopy, *twinCopy){
		"different container": func(a, b *twinCopy) { b.container = "elsewhere" },
		"empty container":     func(a, b *twinCopy) { b.container = "" },
		"short evidence":      func(a, b *twinCopy) { b.plain = "" },
		"nonexact plaintext":  func(a, b *twinCopy) { b.plain += " A unique addition." },
		"quoted HTML":         func(a, b *twinCopy) { b.bodyHTML = "<div>Different content</div>" },
		"two recovered":       func(a, b *twinCopy) { a.quoted = true },
		"two mailbox":         func(a, b *twinCopy) { b.quoted = false },
		"wrong time":          func(a, b *twinCopy) { b.wall = b.wall.Add(5 * time.Minute) },
	} {
		t.Run(name, func(t *testing.T) {
			a, b := mentionCopies()
			change(&a, &b)
			if why, ok := twinnable(a, b); ok {
				t.Fatalf("unsafe pair matched: %s", why)
			}
		})
	}
	a, b := mentionCopies()
	b.words = b.words[:5]
	if groups, _ := groupTwins([]twinCopy{a, b}); len(groups) != 1 {
		t.Fatal("primary short prefilter bypassed proven substantial plaintext")
	}
	a, b = mentionCopies()
	b.atts = 1
	if _, _, _, ok := resolveTwins([]twinCopy{a, b}); ok {
		t.Fatal("recovered attachment veto bypassed")
	}
	a, b = mentionCopies()
	other := a
	other.id = 3
	other.ext = "mail:<competing@example.test>"
	other.prose = append(append([]string{}, a.prose[:10]...), append([]string{"an", "independent", "inline", "addition", "here"}, a.prose[10:]...)...)
	for _, near := range [][]twinCopy{{a, other}, {other, a}} {
		if _, ok := uniqueCopyBase(b, near); ok {
			t.Fatal("competing mailbox candidates guessed")
		}
	}
	if _, _, _, ok := resolveTwins([]twinCopy{a, b, other}); ok {
		t.Fatal("transitive group with two originals collapsed")
	}
	// The original positional and destination evidence remains authoritative.
	edited := b
	edited.prose = append([]string{}, a.prose...)
	edited.prose = append(edited.prose[:10], append([]string{"these", "five", "words", "are", "new"}, edited.prose[10:]...)...)
	if why, inline := annotates(a, edited); why == "" || !inline {
		t.Fatal("inline addition protection changed")
	}
}

func seedMentionTwin(t *testing.T, s *Store) (int64, int64, int64) {
	t.Helper()
	author := person(t, s, "rowan@example.test", "Rowan Example")
	quoter := person(t, s, "quoter@example.test", "Quoter Example")
	a, b := mentionCopies()
	result, err := s.Put(Entry{Source: SourceMail, ExtID: a.ext, TS: a.wall, PersonID: author, Container: a.container, BodyText: mentionPlain, BodyHTML: a.bodyHTML}, &Mail{MessageID: "<mention@example.test>"}, []Attachment{{Name: "inventory.csv", Mime: "text/csv"}})
	if err != nil {
		t.Fatal(err)
	}
	if got, ok, err := FindTwin(s, author, b.wall, mentionPlain, "", a.container); err != nil || !ok || got != result.ID {
		t.Fatalf("ingest mismatch got=%d ok=%v err=%v", got, ok, err)
	}
	if _, ok, err := FindTwin(s, quoter, b.wall, mentionPlain, "", a.container); err != nil || ok {
		t.Fatal("cross-author ingest match", err)
	}
	if _, ok, err := FindTwin(s, author, b.wall, mentionPlain, "same subject", "elsewhere"); err != nil || ok {
		t.Fatal("subject vouched another thread", err)
	}
	child, err := s.Put(Entry{Source: SourceMail, ExtID: "mail:<child@example.test>", TS: b.wall.Add(time.Hour), PersonID: quoter, Container: a.container, BodyText: "Following up on the inventory discussion."}, &Mail{MessageID: "<child@example.test>"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	drop, _, err := s.PutQuoted(Entry{Source: SourceMail, ExtID: b.ext, TS: b.wall, PersonID: author, Container: b.container, BodyText: mentionPlain})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Sight(drop, child.ID, "quoted", "depth 0"); err != nil {
		t.Fatal(err)
	}
	if err = s.SetParent(child.ID, drop); err != nil {
		t.Fatal(err)
	}
	return result.ID, drop, child.ID
}

func TestPlainTwinRepairCacheAndProvenance(t *testing.T) {
	path := filepath.Join(t.TempDir(), "twins.db")
	s := fileFoldStore(t, path)
	keep, drop, child := seedMentionTwin(t, s)
	want, err := CollapseTwins(s, false)
	if err != nil || want.Removed != 1 || len(want.Collapse) != 1 || want.Collapse[0].Keep != keep {
		t.Fatalf("dry plan=%+v err=%v", want, err)
	}
	cold, c := cachedTwinPlan(t, s, true)
	warm, w := cachedTwinPlan(t, s, true)
	if !reflect.DeepEqual(want, cold) || !reflect.DeepEqual(want, warm) || c.reductions == 0 || w.hits != w.inspected {
		t.Fatal("cold/warm fallback parity", c, w)
	}
	s.Close()
	s = fileFoldStore(t, path)
	defer s.Close()
	reopened, r := cachedTwinPlan(t, s, false)
	if !reflect.DeepEqual(want, reopened) || r.hits != r.inspected {
		t.Fatal("persistent evidence lost", r)
	}
	if _, err = s.db.Exec(`update twins_body_cache set format_version=2`); err != nil {
		t.Fatal(err)
	}
	upgraded, u := cachedTwinPlan(t, s, true)
	if !reflect.DeepEqual(want, upgraded) || u.hits != 0 || u.reductions != u.inspected {
		t.Fatal("old format accepted", u)
	}
	if _, err = s.db.Exec(`update entries set body_html=replace(body_html,'mailto:ada@example.test','mailto:other@example.test') where id=?`, keep); err != nil {
		t.Fatal(err)
	}
	changed, miss := cachedTwinPlan(t, s, true)
	if changed.Removed != 0 || miss.reductions != 1 {
		t.Fatal("HTML-only change reused fallback proof", changed, miss)
	}
	if _, err = s.db.Exec(`update entries set body_html=? where id=?`, mentionHTML+mentionHistory, keep); err != nil {
		t.Fatal(err)
	}
	applied, err := CollapseTwins(s, true)
	if err != nil || applied.Removed != 1 {
		t.Fatal("apply", applied, err)
	}
	var body, raw string
	var parent, n int64
	if err = s.db.QueryRow(`select body_text,body_html from entries where id=?`, keep).Scan(&body, &raw); err != nil {
		t.Fatal(err)
	}
	if body != mentionPlain || raw != mentionHTML+mentionHistory {
		t.Fatal("surviving body bytes rewritten")
	}
	if err = s.db.QueryRow(`select parent_id from entries where id=?`, child).Scan(&parent); err != nil || parent != keep {
		t.Fatal("reply edge not preserved", parent, err)
	}
	if err = s.db.QueryRow(`select count(*) from sightings where entry_id=? and seen_in=? and kind='quoted' and detail='depth 0'`, keep, child).Scan(&n); err != nil || n != 1 {
		t.Fatal("provenance not preserved", n, err)
	}
	if err = s.db.QueryRow(`select count(*) from entries where id=?`, drop).Scan(&n); err != nil || n != 0 {
		t.Fatal("duplicate survived", n, err)
	}
	if err = s.db.QueryRow(`select count(*) from attachments where entry_id=?`, keep).Scan(&n); err != nil || n != 1 {
		t.Fatal("mailbox attachment changed", n, err)
	}
}

func TestPlainTwinIngestAmbiguityAndAnnotations(t *testing.T) {
	s := open(t)
	author := person(t, s, "ambiguous@example.test", "Ambiguous Example")
	a, b := mentionCopies()
	for i := 0; i < 2; i++ {
		if _, err := s.Put(Entry{Source: SourceMail, ExtID: fmt.Sprintf("mail:<candidate-%d@example.test>", i), TS: a.wall, PersonID: author, Container: a.container, BodyText: mentionPlain, BodyHTML: a.bodyHTML}, &Mail{MessageID: fmt.Sprintf("<candidate-%d@example.test>", i)}, nil); err != nil {
			t.Fatal(err)
		}
	}
	if _, ok, err := FindTwin(s, author, b.wall, mentionPlain, "", a.container); err != nil || ok {
		t.Fatal("ambiguous originals guessed", err)
	}
	if _, ok, err := FindDerived(s, author, b.wall, mentionPlain, "", a.container); err != nil || ok {
		t.Fatal("ambiguous derived original guessed", err)
	}
	for _, body := range []string{strings.Replace(mentionPlain, "inventory summary", "inventory summary these five words are entirely new", 1), strings.Replace(mentionPlain, "tomorrow?", "tomorrow!", 1), mentionPlain + " https://changed.example.test/Case?x=Y"} {
		if _, ok, err := FindTwin(s, author, b.wall, body, "", a.container); err != nil || ok {
			t.Fatal("edited fallback matched", err)
		}
	}
}

func TestPlainTwinSingleBaseEditedRefusals(t *testing.T) {
	s := open(t)
	author := person(t, s, "single@example.test", "Single Example")
	a, b := mentionCopies()
	if _, err := s.Put(Entry{Source: SourceMail, ExtID: a.ext, TS: a.wall, PersonID: author, Container: a.container, BodyText: mentionPlain, BodyHTML: a.bodyHTML}, &Mail{MessageID: "<mention@example.test>"}, nil); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"inline five words": strings.Replace(mentionPlain, "inventory summary", "inventory summary these five words are entirely new", 1),
		"punctuation":       strings.Replace(mentionPlain, "tomorrow?", "tomorrow!", 1),
		"email address":     strings.Replace(mentionPlain, "ada@example.test", "other@example.test", 1),
		"URL":               mentionPlain + " https://changed.example.test/Case?x=Y",
		"one word":          strings.Replace(mentionPlain, "numbered cartons", "different cartons", 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, ok, err := FindTwin(s, author, b.wall, body, "", a.container); err != nil || ok {
				t.Fatal("modified single-base quote matched", err)
			}
		})
	}
}

func TestPlainTwinOwnHistoryAndBounds(t *testing.T) {
	body := mentionPlain + "\n\nOn Mon, 5 Jan 2026 at 10:00, Previous Example <previous@example.test> wrote:\n> Previous inventory discussion."
	if plainTwinEvidence(body, mentionHTML+mentionHistory, false) != plainTwinText(mentionPlain) {
		t.Fatal("plain quote history was not peeled at the existing ownWords boundary")
	}
	for _, raw := range []string{"<html><body>" + mentionHTML + "</body></html>", "<span>" + mentionHTML + "</span>", strings.Replace(mentionHTML, `dir="ltr"`, `dir=ltr`, 1)} {
		if plainTwinEvidence(mentionPlain, raw, false) != "" {
			t.Fatal("unsupported envelope or unquoted syntax admitted")
		}
	}
	if plainTwinEvidence(strings.Repeat("x", twinPlainMaxBytes+1), mentionHTML, false) != "" {
		t.Fatal("oversize plaintext admitted")
	}
	// Enough address tokens alone cannot vouch for a trivial message.
	short := "+Add @Ada Example <ada@example.test> @Bo Example <bo@example.test> Yes thanks."
	shortHTML := `<div>+Add <a href="mailto:ada@example.test">@Ada Example</a> <a href="mailto:bo@example.test">@Bo Example</a> Yes thanks.</div>`
	if projected, ok := mentionPlainHTML(shortHTML); !ok || plainTwinText(projected) != short {
		t.Fatal("short-message fixture must otherwise prove HTML equivalence")
	}
	if plainTwinEvidence(short, shortHTML, false) != "" {
		t.Fatal("addresses manufactured the meaningful-token floor")
	}
}
