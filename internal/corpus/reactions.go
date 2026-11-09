package corpus

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/zachpmanson/docket/gmail/mail"
)

// Reaction evidence is sender-supplied, not authenticated intent. Event counts
// count source notifications, not a provider's current per-person state.
type Reaction struct {
	Emoji  string          `json:"emoji"`
	Events []ReactionEvent `json:"events"`
}
type ReactionEvent struct {
	SourceExtID string `json:"sourceExtId"`
	ClaimedFrom string `json:"claimedFrom"`
	Date        string `json:"date"`
	Body        string `json:"body"`
	SourceLink  string `json:"sourceLink"`
}
type ReactionEvidence struct {
	SourceDigest string `json:"sourceDigest"`
	Digest       string `json:"digest"`
	Evidence     string `json:"evidence"`
}

// ReactionOnly validates the versioned evidence again at the JSON boundary.
// A layout assertion alone, a claimed emoji, or fallback prose cannot classify.
func ReactionOnly(r *mail.ReactionInspection) bool {
	return mail.ValidateReactionInspection(r) == nil
}

func reactionDigest(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func putReactionEvidence(tx corpusWriter, accountID string, e Entry, m *Mail, id int64) error {
	if m == nil || m.GmailID == "" {
		return nil
	}
	var attachments int
	if err := tx.QueryRow(`select count(*) from attachments where entry_id=?`, id).Scan(&attachments); err != nil {
		return err
	}
	snapshotDigest := reactionSnapshot(e, m, attachments)
	var previous string
	if err := tx.QueryRow(`select current_snapshot from reaction_sources where source_ext_id=?`, e.ExtID).Scan(&previous); err != nil && err != sql.ErrNoRows {
		return err
	}
	// Old decoders cannot erase evidence, but changed source content must revoke
	// its applicability even when no new inspection was supplied.
	if _, err := tx.Exec(`update reaction_sources set entry_id=?,current_snapshot=? where source_ext_id=? and (entry_id is not ? or current_snapshot is not ?)`, id, snapshotDigest, e.ExtID, id, snapshotDigest); err != nil {
		return err
	}
	if m.ReactionInspection == nil && (previous == "" || previous == snapshotDigest) {
		return nil
	}
	raw, err := json.Marshal(m.ReactionInspection)
	if err != nil {
		return err
	}
	candidate := ReactionOnly(m.ReactionInspection) && strings.TrimSpace(m.InReplyTo) == m.ReactionInspection.TargetMessageID && attachments == 0
	emoji, target := "", ""
	if candidate {
		emoji = m.ReactionInspection.Candidates[0].Emoji
		target = m.ReactionInspection.TargetMessageID
	}
	semantic := reactionSemantic(candidate, emoji, target, e, m, attachments)
	if m.ReactionInspection != nil && len(m.ReactionInspection.Candidates) > 0 {
		if _, err = tx.Exec(`insert into reaction_sources(source_ext_id,entry_id,current_snapshot) values(?,?,?)
 on conflict(source_ext_id) do update set entry_id=excluded.entry_id,current_snapshot=excluded.current_snapshot where reaction_sources.entry_id is not excluded.entry_id or reaction_sources.current_snapshot is not excluded.current_snapshot`, e.ExtID, id, snapshotDigest); err != nil {
			return err
		}
	}
	_, err = tx.Exec(`insert into reaction_inspections(account_id,gmail_id,source_ext_id,digest,evidence,semantic,snapshot,candidate,emoji,target_ref,claimed_from,source_date)
 values(?,?,?,?,?,?,?,?,?,?,?,?) on conflict do nothing`, accountID, m.GmailID, e.ExtID, reactionDigest(raw), string(raw), semantic, snapshotDigest, candidate, emoji, target, m.From, m.SourceDate)
	return err
}

// ReactionsFor returns only exact, unambiguous, account-accessible associations.
// Resolution is a view, so late targets and deleted/remapped copies cannot leave
// a stale persisted target pointer. The caller only suppresses sources whose
// targets are present in the same response.
func (s *Store) ReactionsFor(accountID string, extIDs []string) (map[string][]Reaction, map[string]string, error) {
	groups := map[string][]Reaction{}
	hidden := map[string]string{}
	if len(extIDs) == 0 {
		return groups, hidden, nil
	}
	ph, args := placeholders(extIDs)
	args = append(args, accountID, accountID)
	rows, err := s.db.Query(`select a.source_ext_id,t.ext_id,i.emoji,i.claimed_from,i.source_date,coalesce(e.body_text,''),i.gmail_id,coalesce(ga.email,'')
 from reaction_active a join entries t on t.id=a.target_id join entries e on e.id=a.source_id
 join reaction_inspections i on i.source_ext_id=a.source_ext_id
 join gmail_copies sc on sc.account_id=i.account_id and sc.gmail_id=i.gmail_id and sc.entry_id=a.source_id
 join gmail_copies tc on tc.entry_id=a.target_id and tc.account_id=sc.account_id
 join gmail_accounts ga on ga.id=sc.account_id
 where t.ext_id in (`+ph+`) and (?='' or sc.account_id=?)
 order by t.ext_id,i.emoji,a.source_ext_id,i.account_id,i.gmail_id`, args...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	seen := map[string]bool{}
	for rows.Next() {
		var source, target, emoji, from, date, body, gmail, email string
		if err = rows.Scan(&source, &target, &emoji, &from, &date, &body, &gmail, &email); err != nil {
			return nil, nil, err
		}
		if seen[source] {
			continue
		}
		seen[source] = true
		event := ReactionEvent{SourceExtID: source, ClaimedFrom: from, Date: date, Body: body, SourceLink: GmailMessageURL(gmail, email)}
		gs := groups[target]
		slot := -1
		for n := range gs {
			if gs[n].Emoji == emoji {
				slot = n
				break
			}
		}
		if slot < 0 {
			gs = append(gs, Reaction{Emoji: emoji, Events: []ReactionEvent{}})
			slot = len(gs) - 1
		}
		gs[slot].Events = append(gs[slot].Events, event)
		groups[target] = gs
		hidden[source] = target
	}
	return groups, hidden, rows.Err()
}

func (s *Store) ReactionEvidence(extID, accountID string) ([]ReactionEvidence, error) {
	rows, err := s.db.Query(`select distinct i.digest,i.snapshot,i.evidence from reaction_inspections i
 join entries r on r.ext_id=i.source_ext_id
 join gmail_copies c on c.entry_id=r.id and c.account_id=i.account_id and c.gmail_id=i.gmail_id
 where i.source_ext_id=? and (?='' or i.account_id=?) order by i.digest`, extID, accountID, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ReactionEvidence
	for rows.Next() {
		var e ReactionEvidence
		if err = rows.Scan(&e.Digest, &e.SourceDigest, &e.Evidence); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) EntryAccessible(extID, accountID string) (bool, error) {
	if accountID == "" {
		return true, nil
	}
	var ok bool
	err := s.db.QueryRow(`select exists(select 1 from entries e where e.ext_id=? and
 (exists(select 1 from gmail_copies c where c.entry_id=e.id and c.account_id=?) or
 exists(select 1 from sightings g join gmail_copies c on c.entry_id=g.seen_in where g.entry_id=e.id and c.account_id=?)))`, extID, accountID, accountID).Scan(&ok)
	return ok, err
}

// ReactionPrecondition binds a reviewed preview to unchanged local source,
// copy, evidence history, target and quote provenance. It is not a heuristic.
func (s *Store) ReactionPrecondition(accountID, gmailID string) (string, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	return reactionPrecondition(tx, accountID, gmailID)
}
func reactionPrecondition(q corpusQuery, accountID, gmailID string) (string, error) {
	var id int64
	if err := q.QueryRow(`select entry_id from gmail_copies where account_id=? and gmail_id=?`, accountID, gmailID).Scan(&id); err != nil {
		return "", err
	}
	// Deliberately bind the graph revision too: it includes quote/sender repairs.
	var revision int64
	if err := q.QueryRow(`select source_revision from reconcile_state where id=1`).Scan(&revision); err != nil {
		return "", err
	}
	var parts []string
	for _, query := range []string{
		`select json_array(e.ext_id,e.body_text,e.body_html,e.ts,m.message_id,m.in_reply_to,m.from_addr) from entries e join mail_detail m on m.entry_id=e.id where e.id=?`,
		`select json_array(account_id,gmail_id,digest,semantic) from reaction_inspections where source_ext_id=(select ext_id from entries where id=?) order by account_id,gmail_id,digest`,
		`select json_array(reference,entry_id) from reaction_message_refs where reference in (select reference from reaction_message_refs where entry_id=?) order by reference,entry_id`,
	} {
		rows, err := q.Query(query, id)
		if err != nil {
			return "", err
		}
		for rows.Next() {
			var text string
			if err = rows.Scan(&text); err != nil {
				rows.Close()
				return "", err
			}
			parts = append(parts, text)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return "", err
		}
	}
	sort.Strings(parts)
	return reactionDigest([]byte(fmt.Sprint(revision) + "\x00" + strings.Join(parts, "\x00"))), nil
}

// ReactionStatus explains why a retained source has not become a chip.
func (s *Store) ReactionStatus(extID, accountID string) (string, error) {
	evidence, err := s.ReactionEvidence(extID, accountID)
	if err != nil {
		return "", err
	}
	if len(evidence) == 0 {
		return "not-inspected", nil
	}
	var conflict, agreed, resolved, active bool
	err = s.db.QueryRow(`select
 (select count(distinct semantic)>1 from reaction_inspections where source_ext_id=?),
 exists(select 1 from reaction_agreed where source_ext_id=?),
 exists(select 1 from reaction_resolved where source_ext_id=?),
 exists(select 1 from reaction_active where source_ext_id=?)`, extID, extID, extID, extID).Scan(&conflict, &agreed, &resolved, &active)
	if err != nil {
		return "", err
	}
	if conflict {
		return "conflict", nil
	}
	if !agreed {
		return "ordinary", nil
	}
	if !resolved {
		return "unresolved-missing-ambiguous-inaccessible-or-self", nil
	}
	if !active {
		return "fallback-quote-artifacts-or-reaction-target", nil
	}
	var target string
	err = s.db.QueryRow(`select t.ext_id from reaction_active a join entries t on t.id=a.target_id where a.source_ext_id=?`, extID).Scan(&target)
	if err != nil {
		return "", err
	}
	groups, _, err := s.ReactionsFor(accountID, []string{target})
	if err != nil {
		return "", err
	}
	if len(groups[target]) == 0 {
		return "unresolved-inaccessible", nil
	}
	return "resolved", nil
}

func accountEntrySQL(alias, accountID string) (string, []any) {
	if accountID == "" {
		return "1", nil
	}
	return `(exists(select 1 from gmail_copies ac where ac.entry_id=` + alias + `.id and ac.account_id=?) or exists(select 1 from sightings ag join gmail_copies ac on ac.entry_id=ag.seen_in where ag.entry_id=` + alias + `.id and ac.account_id=?))`, []any{accountID, accountID}
}

func reactionSnapshot(e Entry, m *Mail, attachments int) string {
	snapshot, _ := json.Marshal([]any{m.From, m.InReplyTo, m.MessageID, e.TS.Unix(), e.BodyText, e.BodyHTML, attachments})
	return reactionDigest(snapshot)
}

func reactionSemantic(candidate bool, emoji, target string, e Entry, m *Mail, attachments int) string {
	raw, _ := json.Marshal([]any{candidate, emoji, target, m.From, m.SourceDate, e.BodyText, e.BodyHTML, m.MessageID, m.InReplyTo, e.TS.Unix(), attachments})
	return reactionDigest(raw)
}
