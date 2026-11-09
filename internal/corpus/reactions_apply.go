package corpus

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	netmail "net/mail"
	"strings"
	"time"

	"github.com/zachpmanson/docket/gmail/mail"
)

const ReactionPreviewLimit = 20

type ReactionSource struct {
	ExtID     string `json:"extId"`
	MessageID string `json:"messageId"`
	InReplyTo string `json:"inReplyTo"`
	From      string `json:"from"`
	TS        int64  `json:"ts"`
	Body      string `json:"body"`
	BodyHTML  string `json:"bodyHTML"`
}
type ReactionPreviewItem struct {
	GmailID        string                   `json:"gmailId"`
	Source         ReactionSource           `json:"source"`
	Precondition   string                   `json:"precondition"`
	Date           string                   `json:"date"`
	Inspection     *mail.ReactionInspection `json:"inspection"`
	EvidenceDigest string                   `json:"evidenceDigest"`
	TargetExtID    string                   `json:"targetExtId,omitempty"`
	Status         string                   `json:"status"`
	QuoteSightings int                      `json:"quoteSightings"`
	Refusal        string                   `json:"refusal,omitempty"`
}
type ReactionManifest struct {
	Version   int                   `json:"version"`
	AccountID string                `json:"accountId"`
	Items     []ReactionPreviewItem `json:"items"`
}

func reactionSource(q corpusQuery, accountID, gmailID string) (ReactionSource, int64, error) {
	var r ReactionSource
	var id int64
	err := q.QueryRow(`select e.id,e.ext_id,coalesce(m.message_id,''),coalesce(m.in_reply_to,''),coalesce(m.from_addr,''),e.ts,coalesce(e.body_text,''),coalesce(e.body_html,'')
 from gmail_copies c join entries e on e.id=c.entry_id join mail_detail m on m.entry_id=e.id
 where c.account_id=? and c.gmail_id=?`, accountID, gmailID).Scan(&id, &r.ExtID, &r.MessageID, &r.InReplyTo, &r.From, &r.TS, &r.Body, &r.BodyHTML)
	return r, id, err
}

func reactionQuoteCheck(q corpusQuery, id int64) (int, error) {
	var count, orphans int
	err := q.QueryRow(`select count(*),coalesce(sum(case when not exists(select 1 from sightings independent
 where independent.entry_id=g.entry_id and (independent.seen_in is null or independent.seen_in!=g.seen_in)) then 1 else 0 end),0)
 from sightings g where g.seen_in=?`, id).Scan(&count, &orphans)
	if err != nil {
		return 0, err
	}
	if orphans != 0 {
		return count, fmt.Errorf("refusing %d uniquely source-sighted quote artifacts; retain ordinary fallback", orphans)
	}
	return count, nil
}

// PreviewReactionSource reads only local state. The MIME read is supplied by the
// normal mailbox boundary; no quote extraction, labels, cursors or Put runs here.
func (s *Store) PreviewReactionSource(accountID, gmailID string) (ReactionPreviewItem, error) {
	var item ReactionPreviewItem
	item.GmailID = gmailID
	tx, err := s.db.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return item, err
	}
	defer tx.Rollback()
	if err = requireSchema(tx, len(migrations)); err != nil {
		return item, err
	}
	var id int64
	item.Source, id, err = reactionSource(tx, accountID, gmailID)
	if err != nil {
		return item, err
	}
	item.Precondition, err = reactionPrecondition(tx, accountID, gmailID)
	if err != nil {
		return item, err
	}
	item.QuoteSightings, err = reactionQuoteCheck(tx, id)
	if err != nil {
		item.Refusal = err.Error()
	}
	return item, nil
}

// PreviewReactionTarget never uses References, graph position or body prose.
func (s *Store) PreviewReactionTarget(accountID, sourceExtID, targetRef string) (string, string, error) {
	rows, err := s.db.Query(`select distinct e.ext_id,exists(select 1 from reaction_agreed r where r.source_id=e.id),exists(select 1 from gmail_copies selected where selected.entry_id=e.id and selected.account_id=?)
 from reaction_message_refs r join entries e on e.id=r.entry_id join gmail_copies c on c.entry_id=e.id
 where r.reference=? and exists(select 1 from gmail_copies sourceCopy join entries sourceEntry on sourceEntry.id=sourceCopy.entry_id where sourceEntry.ext_id=? and sourceCopy.account_id=c.account_id) order by e.ext_id`, accountID, targetRef, sourceExtID)
	if err != nil {
		return "", "", err
	}
	defer rows.Close()
	var targets []string
	var reactionTarget, selectedAccessible bool
	for rows.Next() {
		var ext string
		var reaction, accessible bool
		if err = rows.Scan(&ext, &reaction, &accessible); err != nil {
			return "", "", err
		}
		targets = append(targets, ext)
		reactionTarget = reactionTarget || reaction
		selectedAccessible = selectedAccessible || accessible
	}
	if err = rows.Err(); err != nil {
		return "", "", err
	}
	if len(targets) != 1 {
		return "", "unresolved-missing-ambiguous-or-inaccessible", nil
	}
	if !selectedAccessible {
		return "", "unresolved-inaccessible", nil
	}
	if targets[0] == sourceExtID {
		return "", "unresolved-self", nil
	}
	if reactionTarget {
		return "", "unresolved-reaction-target", nil
	}
	return targets[0], "resolved", nil
}

func InspectionDigest(r *mail.ReactionInspection) string {
	raw, _ := json.Marshal(r)
	return reactionDigest(raw)
}

// ApplyReactionManifest changes only sidecars and explicitly reviewed source-
// origin sightings with independent evidence. Every precondition is checked
// before any write, and the audit row makes exact replay a no-op.
func (s *Store) ApplyReactionManifest(manifest ReactionManifest) (bool, error) {
	if manifest.Version != 1 || manifest.AccountID == "" || len(manifest.Items) == 0 || len(manifest.Items) > ReactionPreviewLimit {
		return false, errors.New("invalid or unbounded reaction manifest")
	}
	unlock, err := s.LockGmailSync(manifest.AccountID)
	if err != nil {
		return false, err
	}
	defer unlock()
	raw, err := json.Marshal(manifest)
	if err != nil {
		return false, err
	}
	digest := reactionDigest(raw)
	tx, err := s.db.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var schemaVersion int
	if err = tx.QueryRow(`select coalesce(max(v),0) from schema_version`).Scan(&schemaVersion); err != nil {
		return false, err
	}
	if schemaVersion != len(migrations) {
		return false, fmt.Errorf("reaction apply requires supported schema %d, got %d", len(migrations), schemaVersion)
	}
	var applied bool
	if err = tx.QueryRow(`select exists(select 1 from reaction_applies where digest=?)`, digest).Scan(&applied); err != nil {
		return false, err
	}
	if applied {
		return false, nil
	}
	ids := map[string]int64{}
	for _, item := range manifest.Items {
		if _, duplicate := ids[item.GmailID]; duplicate {
			return false, errors.New("duplicate source in reaction manifest")
		}
		date, _ := netmail.ParseDate(item.Date)
		if date.Unix() != item.Source.TS {
			return false, errors.New("preview date differs from stored source timestamp")
		}
		if item.Refusal != "" || !ReactionOnly(item.Inspection) || InspectionDigest(item.Inspection) != item.EvidenceDigest || strings.TrimSpace(item.Source.InReplyTo) != item.Inspection.TargetMessageID {
			return false, errors.New("refused or invalid reaction preview")
		}
		source, id, err := reactionSource(tx, manifest.AccountID, item.GmailID)
		if err != nil {
			return false, err
		}
		if err = reactionAgreement(tx, item); err != nil {
			return false, err
		}
		if source != item.Source {
			return false, errors.New("reaction source changed since preview")
		}
		current, err := reactionPrecondition(tx, manifest.AccountID, item.GmailID)
		if err != nil {
			return false, err
		}
		if current != item.Precondition {
			return false, errors.New("reaction evidence/target/provenance changed since preview")
		}
		count, err := reactionQuoteCheck(tx, id)
		if err != nil {
			return false, err
		}
		if count != item.QuoteSightings {
			return false, errors.New("quote sightings changed since preview")
		}
		var attachments int
		if err = tx.QueryRow(`select count(*) from attachments where entry_id=?`, id).Scan(&attachments); err != nil {
			return false, err
		}
		if attachments != 0 {
			return false, errors.New("source has attachments")
		}
		ids[item.GmailID] = id
	}
	// Keep the withdrawn rows in an audit payload for inspection/restoration.
	var withdrawn []string
	for _, item := range manifest.Items {
		id := ids[item.GmailID]
		rows, err := tx.Query(`select json_array(entry_id,seen_in,kind,detail) from sightings where seen_in=? order by entry_id,kind`, id)
		if err != nil {
			return false, err
		}
		for rows.Next() {
			var row string
			if err = rows.Scan(&row); err != nil {
				rows.Close()
				return false, err
			}
			withdrawn = append(withdrawn, row)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return false, err
		}
		if _, err = tx.Exec(`delete from sightings where seen_in=?`, id); err != nil {
			return false, err
		}
		e := Entry{ExtID: item.Source.ExtID, BodyText: item.Source.Body, BodyHTML: item.Source.BodyHTML, TS: time.Unix(item.Source.TS, 0)}
		m := &Mail{GmailID: item.GmailID, MessageID: item.Source.MessageID, InReplyTo: item.Source.InReplyTo, From: item.Source.From, SourceDate: item.Date, ReactionInspection: item.Inspection}
		if err = putReactionEvidence(tx, manifest.AccountID, e, m, id); err != nil {
			return false, err
		}
	}
	audit, _ := json.Marshal(withdrawn)
	if _, err = tx.Exec(`insert into reaction_applies(digest,manifest,withdrawn_sightings,applied_at) values(?,?,?,?)`, digest, string(raw), string(audit), time.Now().Unix()); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

func (s *Store) PreviewReactionAgreement(item ReactionPreviewItem) error {
	return reactionAgreement(s.db, item)
}

func reactionAgreement(q corpusQuery, item ReactionPreviewItem) error {
	if !ReactionOnly(item.Inspection) {
		return errors.New("unsupported reaction inspection")
	}
	e := Entry{BodyText: item.Source.Body, BodyHTML: item.Source.BodyHTML, TS: time.Unix(item.Source.TS, 0)}
	m := &Mail{From: item.Source.From, SourceDate: item.Date, MessageID: item.Source.MessageID, InReplyTo: item.Source.InReplyTo}
	semantic := reactionSemantic(true, item.Inspection.Candidates[0].Emoji, item.Inspection.TargetMessageID, e, m, 0)
	var conflict bool
	if err := q.QueryRow(`select exists(select 1 from reaction_inspections where source_ext_id=? and semantic!=?)`, item.Source.ExtID, semantic).Scan(&conflict); err != nil {
		return err
	}
	if conflict {
		return errors.New("conflicting prior source inspection; retain ordinary fallback")
	}
	return nil
}
