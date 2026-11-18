package corpus

import (
	"strings"
	"testing"
	"time"
)

const sourceMentionMIME = "@Neri <neri@invented.example.test> confirmed; reserve seven baskets. Please review the greenhouse inventory plan before the volunteer session. Keep the damaged containers separate and record the replacement labels on our shared planning sheet. Review <https://inventory.example.test/original.png> before collection.\n\nItem Quantity Shelf\nSeed trays 47 North\nClay pots 83 South"
const sourceColumnMetadata = `<colgroup><col width="120"><col style="width:80px"><col span="1"><col><col><col><col><col></colgroup>`
const sourceMentionHTML = `<div><p><a href="mailto:neri@invented.example.test">@Neri</a> confirmed; reserve seven baskets. Please review the greenhouse inventory plan before the volunteer session. Keep the damaged containers separate and record the replacement labels on our shared planning sheet. Review &lt;https://inventory.example.test/original.png&gt; before collection.</p><table>` + sourceColumnMetadata + `<tr><th>Item</th><th>Quantity</th><th>Shelf</th></tr><tr><td>Seed trays</td><td>47</td><td>North</td></tr><tr><td>Clay pots</td><td>83</td><td>South</td></tr></table></div>`

func TestSourceTwinExactHTMLRepresentations(t *testing.T) {
	b := sourceTwinRepresentations(sourceMentionMIME, sourceMentionHTML)
	labels := strings.ReplaceAll(sourceMentionMIME, " <neri@invented.example.test>", "")
	block := strings.NewReplacer("Item Quantity Shelf", "ItemQuantityShelf", "Seed trays 47 North", "Seed trays47North", "Clay pots 83 South", "Clay pots83South").Replace(labels)
	if b.mime != plainTwinText(sourceMentionMIME) || b.html != plainTwinText(labels) || b.block != plainTwinText(block) {
		t.Fatalf("unexpected original-source projections: %+v", b)
	}
	for _, text := range []string{sourceMentionMIME, labels, block} {
		if !b.matches(text) || b.rendered(text) != (text != sourceMentionMIME) {
			t.Fatalf("exact representation refused: %q", text)
		}
	}
	for _, text := range []string{strings.ReplaceAll(block, "47", "48"), strings.ReplaceAll(block, "reserve", "RESERVE"), strings.ReplaceAll(block, "baskets.", "baskets!"), strings.ReplaceAll(block, "containers separate", "containers please donate our extra tools separate"), block + " <https://inventory.example.test/new-image.png>", block + " extra words"} {
		if b.matches(text) {
			t.Fatalf("non-exact claim accepted: %q", text)
		}
	}
	// A visible image URL remains literal content; it is not a placeholder to
	// erase. Unsupported HTML cannot acquire the table-specific projection.
	image := sourceTwinRepresentations("", `<div>Plan &lt;https://inventory.example.test/original.png&gt;</div>`)
	if !image.matches("Plan <https://inventory.example.test/original.png>") || image.matches("Plan <https://inventory.example.test/replacement.jpg>") || image.matches("Plan") {
		t.Fatalf("visible image URL lost: %+v", image)
	}
	unknown := sourceTwinRepresentations("", `<div><table><tr><td>Value</td><td><unknown>47</unknown></td></tr></table></div>`)
	if unknown.block != "" {
		t.Fatal("unknown content acquired block projection")
	}
}

func TestSourceTwinTextlessColumnMetadata(t *testing.T) {
	without := strings.Replace(sourceMentionHTML, sourceColumnMetadata, "", 1)
	want := sourceTwinRepresentations(sourceMentionMIME, without)
	got := sourceTwinRepresentations(sourceMentionMIME, sourceMentionHTML)
	if want.block == "" || got != want {
		t.Fatalf("textless columns changed exact source representations: got %+v want %+v", got, want)
	}
	for name, own := range map[string]string{
		"image":   `<img src="https://images.example.test/new-own.png">`,
		"unknown": `<unknown>Unique new own instructions.</unknown>`,
		"prose":   `<p>Unique new own instructions.</p>`,
		"select":  `<select><div></select><img src="https://images.example.test/new-own.png"></div>`,
		"foreign": `<svg><foreignObject><select><div></select></foreignObject></svg><img src="https://images.example.test/new-own.png"></div>`,
	} {
		t.Run(name, func(t *testing.T) {
			raw := strings.Replace(sourceMentionHTML, `</colgroup>`, own+`</colgroup>`, 1)
			b := sourceTwinRepresentations(sourceMentionMIME, raw)
			if b.block == want.block || (name != "prose" && b.block != "") {
				t.Fatalf("content hidden by parsed column metadata: %+v", b)
			}
			if name == "prose" && !strings.Contains(b.block, "Unique new own instructions.") {
				t.Fatalf("foster-parented own prose lost: %+v", b)
			}
		})
	}
}

func TestSourceTwinMixedOriginalRepresentationsAndRefusals(t *testing.T) {
	for _, mutation := range []string{"none", "changed cell", "changed visible URL", "inserted prose", "wrong author", "different account", "missing raw parent"} {
		t.Run(mutation, func(t *testing.T) {
			s := open(t)
			f := seedSourceTwin(t, s, sourceMentionMIME)
			sourceExec(t, s, `update entries set body_html=? where id=?`, sourceMentionHTML, f.target)
			b := sourceTwinRepresentations(sourceMentionMIME, sourceMentionHTML)
			// The stored copy and a nested claim use the exact original HTML's
			// joined-cell rendition, while the immediate source uses MIME.
			f.rec.Block.Text = b.block
			sourceExec(t, s, `update entries set body_text=? where id=?`, b.block, f.quote)
			body := "A later volunteer update.\n\nOn Fri, 15 May 2037 at 17:00, Kel Aspen <kel@invented.example.test> wrote:\n> The covered bench is ready.\n>\n> On Thu, 14 May 2037 at 15:00, Fen Larch <fen@invented.example.test> wrote:\n>> " + b.block
			switch mutation {
			case "changed cell":
				body = strings.ReplaceAll(body, "47North", "48North")
			case "changed visible URL":
				body = strings.ReplaceAll(body, "original.png", "replacement.jpg")
			case "inserted prose":
				body = strings.ReplaceAll(body, "containers separate", "containers please donate our extra tools separate")
			case "wrong author":
				body = strings.ReplaceAll(body, "fen@invented.example.test", "kel@invented.example.test")
			case "missing raw parent":
				sourceExec(t, s, `update mail_detail set in_reply_to='' where entry_id=?`, f.host)
			}
			r, err := s.Put(Entry{Source: SourceMail, ExtID: "mail:invented-rendered-host", TS: f.rec.Sent.Add(time.Hour), PersonID: f.other, Container: "invented-reservation", BodyText: body}, &Mail{MessageID: "<invented-rendered-host@invented.example.test>", InReplyTo: "<unrelated-parent@invented.example.test>"}, nil)
			if err != nil {
				t.Fatal(err)
			}
			account := "legacy"
			if mutation == "different account" {
				account = "invented-other"
				if err := s.PutGmailAccount(GmailAccount{ID: account, DisplayName: "Invented other"}); err != nil {
					t.Fatal(err)
				}
			}
			if err := s.PutGmailCopy(GmailCopy{AccountID: account, GmailID: "invented-rendered-host", EntryID: r.ID}); err != nil {
				t.Fatal(err)
			}
			if err := s.Sight(f.quote, r.ID, "quoted", ""); err != nil {
				t.Fatal(err)
			}
			want := mutation == "none"
			assertSourceMatch(t, s, f, want)
			assertCachedOracle(t, s)
		})
	}
}

func TestSourceTwinRefusesDistantStatedGenericReply(t *testing.T) {
	for _, gap := range []time.Duration{27 * time.Hour, 7 * 24 * time.Hour, 365 * 24 * time.Hour} {
		t.Run(gap.String(), func(t *testing.T) {
			s := open(t)
			f := seedSourceTwin(t, s, "Confirmed; reserve seven baskets.")
			sourceExec(t, s, `update entries set ts=? where id=?`, f.rec.Sent.Add(gap).Unix(), f.target)
			assertSourceMatch(t, s, f, false)
		})
	}
}

func TestSourceTwinRenderedCopyAttachmentVeto(t *testing.T) {
	s := open(t)
	f := seedSourceTwin(t, s, sourceMentionMIME)
	sourceExec(t, s, `update entries set body_html=? where id=?`, sourceMentionHTML, f.target)
	b := sourceTwinRepresentations(sourceMentionMIME, sourceMentionHTML)
	sourceExec(t, s, `update entries set body_text=? where id=?`, b.block, f.quote)
	sourceExec(t, s, `insert into attachments(entry_id,name) values(?,'invented-inventory.csv')`, f.quote)
	proof, err := sourceTwinForStored(s.db, f.quote, f.author, "invented-reservation")
	if err != nil || proof.target != f.target || !proof.rendered {
		t.Fatalf("fixture lacks rendered source proof: %+v %v", proof, err)
	}
	p, err := CollapseTwins(s, true)
	if err != nil || p.Removed != 0 {
		t.Fatalf("rendered proof bypassed attachment veto: %+v %v", p, err)
	}
	var n int
	if err := s.db.QueryRow(`select count(*) from attachments where entry_id=? and name='invented-inventory.csv'`, f.quote).Scan(&n); err != nil || n != 1 {
		t.Fatalf("attachment lost: %d %v", n, err)
	}
}
