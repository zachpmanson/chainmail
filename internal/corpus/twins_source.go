package corpus

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/zachpmanson/chainmail/internal/unnest"
)

// Fresh source proof is separate from body-reduction cache evidence.
type sourceTwinProof struct {
	target   int64
	rendered bool
}

func sourceTwinTarget(q corpusQuery, required []int64, key, text, container string, person int64) (sourceTwinProof, error) {
	if len(required) == 0 || len(required) > DefaultQuoteLimits.Hosts || person == 0 || container == "" || plainTwinText(text) == "" {
		return sourceTwinProof{}, nil
	}
	hosts, mandatory, bodies, err := sourceTwinHosts(q, required, key, text, container, person)
	if err != nil {
		if errors.Is(err, errQuoteLimit) || err.Error() == "missing_host" {
			return sourceTwinProof{}, nil
		}
		return sourceTwinProof{}, err
	}
	budget := &quoteBudget{limits: DefaultQuoteLimits, hosts: map[int64]quoteHost{}}
	type claim struct {
		host int64
		rec  unnest.Recovered
	}
	var claims []claim
	var target int64
	var targetTime time.Time
	for _, host := range hosts {
		h, err := budget.host(context.Background(), q, host)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) || errors.Is(err, errQuoteLimit) {
				return sourceTwinProof{}, nil
			}
			return sourceTwinProof{}, err
		}
		matches, top := 0, 0
		var rec unnest.Recovered
		for _, r := range h.quotes {
			if r.Block.Depth == 0 {
				top++
			}
			if r.Key == key {
				matches++
				rec = r
			}
		}
		// Unrelated dated sightings may be discovered on a MIME/HTML-exact
		// mailbox. Required hosts, however, cannot lose or change their key.
		if matches == 0 && !mandatory[host] {
			continue
		}
		if matches != 1 || !recoveredAuthor(q, rec, person) {
			return sourceTwinProof{}, nil
		}
		claims = append(claims, claim{host, rec})
		if rec.Block.Depth > 0 {
			continue
		}
		if top != 1 || len(h.quotes) == 0 || rec.Block.Start != h.quotes[0].Block.Start || rec.Block.Kind != unnest.KindAttribution {
			return sourceTwinProof{}, nil
		}
		rows, err := q.Query(`select distinct t.id,coalesce(t.person_id,0),coalesce(t.container,''),t.ts,
   (t.source='mail' and t.quoted=0 and t.derived=0 and `+strings.ReplaceAll(nonReactionTwinSQL, "e.id", "t.id")+`)
   from entries h join mail_detail hd on hd.entry_id=h.id
   join gmail_copies hc on hc.entry_id=h.id
   join gmail_copies tc on tc.account_id=hc.account_id
   join entries t on t.id=tc.entry_id join mail_detail td on td.entry_id=t.id
   where h.id=? and h.source='mail' and h.quoted=0 and h.derived=0 and h.container=?
   and hd.in_reply_to<>'' and hd.in_reply_to=td.message_id and td.message_id<>''
   limit ?`, host, container, DefaultQuoteLimits.Hosts+1)
		if err != nil {
			return sourceTwinProof{}, err
		}
		count := 0
		var id, author, ts int64
		var thread string
		var eligible bool
		for rows.Next() {
			count++
			if err = rows.Scan(&id, &author, &thread, &ts, &eligible); err != nil {
				rows.Close()
				return sourceTwinProof{}, err
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return sourceTwinProof{}, err
		}
		// Count raw-ID candidates before comparing ANY representation. Content
		// cannot disambiguate duplicate Message-IDs in a shared account.
		if count != 1 || !eligible || id == host || author != person || thread != container || (target != 0 && target != id) {
			return sourceTwinProof{}, nil
		}
		target, targetTime = id, time.Unix(ts, 0).UTC()
	}
	body, ok := bodies[target]
	if target == 0 || !ok || !body.matches(text) {
		return sourceTwinProof{}, nil
	}
	for _, c := range claims {
		// Different requoters may use different exact renditions of this
		// actual mailbox. Never normalise addresses or join words in a claim.
		if !body.matches(c.rec.Block.Text) || (!c.rec.Sent.IsZero() && c.rec.Sent.Sub(targetTime).Abs() > 26*time.Hour) {
			return sourceTwinProof{}, nil
		}
		if c.rec.Block.Depth == 0 {
			continue
		}
		var shared bool
		err = q.QueryRow(`select exists(select 1 from entries h join mail_detail hd on hd.entry_id=h.id
   join gmail_copies hc on hc.entry_id=h.id join gmail_copies tc on tc.account_id=hc.account_id
   where h.id=? and h.source='mail' and h.quoted=0 and h.derived=0 and h.container=? and tc.entry_id=?)`, c.host, container, target).Scan(&shared)
		if err != nil {
			return sourceTwinProof{}, err
		}
		if !shared {
			return sourceTwinProof{}, nil
		}
	}
	return sourceTwinProof{target, body.rendered(text)}, nil
}

// Discover current witnesses attached to exact original-mailbox MIME or HTML
// renditions. Each bounded original HTML part is parsed once for this proof.
func sourceTwinHosts(q corpusQuery, required []int64, key, text, container string, person int64) ([]int64, map[int64]bool, map[int64]sourceTwinBody, error) {
	rows, err := q.Query(`select e.id,
  case when length(cast(coalesce(e.body_text,'') as blob))+length(cast(coalesce(e.body_html,'') as blob))<=? then coalesce(e.body_text,'') else '' end,
  case when length(cast(coalesce(e.body_text,'') as blob))+length(cast(coalesce(e.body_html,'') as blob))<=? then coalesce(e.body_html,'') else '' end,
  length(cast(coalesce(e.body_text,'') as blob))+length(cast(coalesce(e.body_html,'') as blob)),e.quoted,e.ext_id from entries e
  where e.source='mail' and e.person_id=? and e.container=? and
  (e.ext_id=? or (e.quoted=0 and exists(select 1 from mail_detail m where m.entry_id=e.id)))
  order by e.id limit ?`, DefaultQuoteLimits.Bytes, DefaultQuoteLimits.Bytes, person, container, key, DefaultQuoteLimits.Hosts+1)
	if err != nil {
		return nil, nil, nil, err
	}
	type candidate struct {
		id       int64
		required bool
	}
	var entries []candidate
	bodies := map[int64]sourceTwinBody{}
	count := 0
	bytes := int64(0)
	for rows.Next() {
		var id, size int64
		var body, rawHTML, ext string
		var quoted bool
		if err = rows.Scan(&id, &body, &rawHTML, &size, &quoted, &ext); err != nil {
			rows.Close()
			return nil, nil, nil, err
		}
		count++
		bytes += size
		if count > DefaultQuoteLimits.Hosts || bytes > DefaultQuoteLimits.Bytes {
			rows.Close()
			return nil, nil, nil, errQuoteLimit
		}
		b := sourceTwinBody{mime: plainTwinText(body)}
		if !quoted {
			b = sourceTwinRepresentations(body, rawHTML)
			bodies[id] = b
		}
		if ext == key || b.matches(text) {
			entries = append(entries, candidate{id, ext == key})
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, nil, nil, err
	}
	seen := map[int64]bool{}
	mandatory := map[int64]bool{}
	var hosts []int64
	add := func(id int64, required bool) {
		if required {
			mandatory[id] = true
		}
		if !seen[id] {
			seen[id] = true
			hosts = append(hosts, id)
		}
	}
	for _, id := range required {
		add(id, true)
	}
	for _, e := range entries {
		ids, _, err := hostSightings(q, e.id, DefaultQuoteLimits.Hosts)
		if err != nil {
			return nil, nil, nil, err
		}
		for _, h := range ids {
			add(h, e.required)
		}
		if len(hosts) > DefaultQuoteLimits.Hosts {
			return nil, nil, nil, errQuoteLimit
		}
	}
	return hosts, mandatory, bodies, nil
}

func sourceTwinForStored(q corpusQuery, id, person int64, container string) (sourceTwinProof, error) {
	hosts, _, err := hostSightings(q, id, DefaultQuoteLimits.Hosts)
	if err != nil {
		if errors.Is(err, errQuoteLimit) || err.Error() == "missing_host" {
			return sourceTwinProof{}, nil
		}
		return sourceTwinProof{}, err
	}
	r, err := readQuoteRow(q, id, true)
	if err != nil {
		return sourceTwinProof{}, err
	}
	if !r.Quoted || r.Authority || r.Derived {
		return sourceTwinProof{}, nil
	}
	return sourceTwinTarget(q, hosts, r.Ext, r.Body, container, person)
}

// sourceTwinDecision preserves a proven base when the original HTML-derived
// veto recognises an inline edit. Identity alone never authorises data loss.
func sourceTwinDecision(q corpusQuery, host, person int64, rec unnest.Recovered, subject, container string) (int64, DerivedMatch, error) {
	proof, err := sourceTwinTarget(q, []int64{host}, rec.Key, rec.Block.Text, container, person)
	if err != nil || proof.target == 0 {
		return 0, DerivedMatch{}, err
	}
	target := proof.target
	var size int64
	if err := q.QueryRow(`select length(cast(coalesce(body_text,'') as blob))+length(cast(coalesce(body_html,'') as blob)) from entries where id=?`, target).Scan(&size); err != nil {
		return 0, DerivedMatch{}, err
	}
	if size > DefaultQuoteLimits.Bytes {
		return 0, DerivedMatch{}, nil
	}
	base := twinCopy{id: target}
	var body string
	if err := q.QueryRow(`select coalesce(body_text,''),coalesce(body_html,'') from entries where id=?`, target).Scan(&body, &base.bodyHTML); err != nil {
		return 0, DerivedMatch{}, err
	}
	reduceTwinBody(&base, body)
	c := twinCopy{quoted: true, wall: rec.Sent, container: container, subject: subject, sourceTarget: target, sourceRendered: proof.rendered}
	reduceTwinBody(&c, rec.Block.Text)
	if why, inline := annotates(base, c); why != "" {
		if inline {
			return 0, DerivedMatch{Base: target, Why: why}, nil
		}
		return 0, DerivedMatch{}, nil
	}
	return target, DerivedMatch{}, nil
}

// AttachSourceTwin applies current source identity and its sighting together.
// A retained annotated copy is stored and linked to its proven base in the same
// guarded write. Zero means source proof declined; ordinary matching may follow.
func AttachSourceTwin(s *Store, host int64, rec unnest.Recovered, e Entry, detail string) (id int64, created, derived bool, err error) {
	return attachSourceTwin(s, host, rec, e, detail, s.guardedReconcileWrite)
}

func attachSourceTwin(s *Store, host int64, rec unnest.Recovered, e Entry, detail string, write twinWrite) (id int64, created, derived bool, err error) {
	ctx := context.Background()
	for attempt := 0; attempt < 3; attempt++ {
		tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
		if err != nil {
			return 0, false, false, err
		}
		state, err := readReconcileState(tx)
		var target int64
		var d DerivedMatch
		if err == nil {
			target, d, err = sourceTwinDecision(tx, host, e.PersonID, rec, e.Subject, e.Container)
		}
		tx.Rollback()
		if err != nil || (target == 0 && d.Base == 0) {
			return 0, false, false, err
		}
		id, created, derived = target, false, d.Base != 0
		_, err = write(ctx, &state.Source, func(w corpusWriter) error {
			if derived {
				e.Derived = true
				id, created, err = s.putQuoted(w, e)
				if err != nil {
					return err
				}
				if err := persistDerived(w, id, d.Base); err != nil {
					return err
				}
			}
			return sight(w, id, host, "quoted", detail)
		})
		if !errors.Is(err, ErrReconcileConflict) {
			if err != nil {
				return 0, false, false, err
			}
			return id, created, derived, nil
		}
	}
	return 0, false, false, ErrReconcileConflict
}
