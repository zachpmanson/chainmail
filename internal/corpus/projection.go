package corpus

import (
	"database/sql"
	"fmt"
	"net/mail"
	"time"

	"github.com/zachpmanson/chainmail/internal/unnest"
)

// AccountProjection keeps quoted content and provenance tied to selected-mailbox
// evidence. Global enrichment is not authority for a quoted-only account view.
type AccountProjection struct {
	Shown                       Shown
	GmailID, From, To, Cc, HTML string
	AccountEmail                string
	Direct                      bool
	Hosts                       []int64
}

func (s *Store) ProjectForAccount(extID, accountID string) (AccountProjection, error) {
	sh, err := s.Show(extID)
	if err != nil {
		return AccountProjection{}, err
	}
	p := AccountProjection{Shown: sh}
	if accountID == "" {
		return p, nil
	}
	ok, err := s.EntryAccessible(extID, accountID)
	if err != nil {
		return p, err
	}
	if !ok {
		return p, ErrNotFound
	}
	err = s.db.QueryRow(`select c.gmail_id,coalesce(m.from_addr,''),coalesce(m.to_addr,''),coalesce(m.cc_addr,''),coalesce(e.body_html,'')
 from gmail_copies c join entries e on e.id=c.entry_id left join mail_detail m on m.entry_id=e.id
 where c.entry_id=? and c.account_id=? order by c.gmail_id limit 1`, sh.ID, accountID).Scan(&p.GmailID, &p.From, &p.To, &p.Cc, &p.HTML)
	if err != nil && err != sql.ErrNoRows {
		return p, err
	}
	p.Direct = err == nil
	if err = s.db.QueryRow(`select coalesce(email,'') from gmail_accounts where id=?`, accountID).Scan(&p.AccountEmail); err != nil {
		return p, err
	}
	rows, err := s.db.Query(`select g.kind, h.ext_id,coalesce(g.detail,''),h.id,coalesce(h.body_text,''),h.ts from sightings g
 join entries h on h.id=g.seen_in where g.entry_id=? and exists(select 1 from gmail_copies c where c.entry_id=h.id and c.account_id=?) order by h.id,g.kind`, sh.ID, accountID)
	if err != nil {
		return p, err
	}
	var sightings []Sighting
	var recovered []unnest.Recovered
	var placement *int64
	for rows.Next() {
		var g Sighting
		var id, hostTS int64
		var body string
		if err = rows.Scan(&g.Kind, &g.SeenIn, &g.Detail, &id, &body, &hostTS); err != nil {
			rows.Close()
			return p, err
		}
		if placement == nil || hostTS < *placement {
			ts := hostTS
			placement = &ts
		}
		sightings = append(sightings, g)
		p.Hosts = append(p.Hosts, id)
		if !p.Direct {
			var matches []unnest.Recovered
			for _, block := range unnest.Peel(body) {
				if block.Sentinel == "" {
					continue
				}
				rec := unnest.Parse(block)
				if rec.Key == extID || unnest.TextKey(block.Text) == extID {
					matches = append(matches, rec)
				}
			}
			if len(matches) == 1 {
				recovered = append(recovered, matches[0])
			}
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return p, err
	}
	p.Shown.Sightings = sightings
	if p.Direct {
		p.Shown.Quoted = false
		// Thread IDs are account-local too, but copies retain only message IDs.
		p.Shown.Container = ""
		p.Shown.Sightings = append(p.Shown.Sightings, Sighting{Kind: "direct"})
		p.Shown.Permalink = GmailMessageURL(p.GmailID, p.AccountEmail)
		p.Shown.GmailAccountEmail = p.AccountEmail
		return p, nil
	}
	// Do not reuse global headers, longer text, original HTML, attachments or cast
	// when this account holds only a quote of the logical message.
	p.Shown.Quoted = true
	if placement == nil {
		return p, ErrNotFound
	}
	p.Shown.TS = time.Unix(*placement, 0)
	p.Shown.TZ = ""
	p.Shown.TZOffset = nil
	p.Shown.Author = ""
	p.Shown.Subject = ""
	p.Shown.Body = "Content unavailable in this account's quote evidence. Original date unavailable; placement uses an accessible host's timestamp."
	p.Shown.PersonID = 0
	p.Shown.PreferOriginal = false
	p.Shown.PreferOriginalSet = false
	p.Shown.HasOriginal = false
	p.Shown.Attachments = nil
	p.Shown.Participants = nil
	p.Shown.ToRecipients = nil
	p.Shown.CcRecipients = nil
	p.Shown.Permalink = ""
	p.Shown.Container = ""
	p.Shown.ParentRef = ""
	if p.Shown.Parent != "" {
		accessible, e := s.EntryAccessible(p.Shown.Parent, accountID)
		if e != nil {
			return p, e
		}
		if !accessible {
			p.Shown.Parent = ""
		}
	}
	// Multiple independently accessible copies may enrich one another, but never
	// ambiguous blocks within a host. Dedup uses the extractor's existing keys.
	rs := unnest.Dedup(recovered)
	if len(rs) == 1 {
		r := rs[0]
		p.Shown.Body = r.Block.Text
		p.Shown.Author = r.Sender
		if p.Shown.Author == "" {
			p.Shown.Author = r.Address
		}
		p.Shown.Subject = r.Subject
		p.To = r.To
		p.Cc = r.Cc
		if r.Address != "" {
			p.From = (&mail.Address{Name: r.Sender, Address: r.Address}).String()
		}
		if !r.Sent.IsZero() {
			p.Shown.TS = r.Sent
			p.Shown.TZ = r.TZ
			p.Shown.TZOffset = nil
		}
		p.Shown.ToRecipients = ParseAddresses(r.To)
		p.Shown.CcRecipients = ParseAddresses(r.Cc)
	}
	return p, nil
}

func (s *Store) ShowForAccount(extID, accountID string) (Shown, error) {
	if accountID == "" {
		return s.Show(extID)
	}
	p, err := s.ProjectForAccount(extID, accountID)
	if err != nil {
		return Shown{}, fmt.Errorf("projecting %s: %w", extID, err)
	}
	return p.Shown, nil
}
