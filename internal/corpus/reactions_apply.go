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
const ReactionSourceByteLimit = 4 << 20
const ReactionPreviewByteLimit = 16 << 20

func reactionSourceBytes(r ReactionSource) int {
	return len(r.ExtID) + len(r.MessageID) + len(r.InReplyTo) + len(r.From) + len(r.Body) + len(r.BodyHTML)
}

// Validate is database-independent and must run before opening a writer.
func (m ReactionManifest) Validate() error {
	if m.Version != 1 || strings.TrimSpace(m.AccountID) == "" || strings.TrimSpace(m.AccountID) != m.AccountID || len(m.Items) == 0 || len(m.Items) > ReactionPreviewLimit {
		return errors.New("invalid or unbounded reaction manifest")
	}
	seen := map[string]bool{}
	total := 0
	for _, item := range m.Items {
		size := reactionSourceBytes(item.Source)
		total += size
		if size > ReactionSourceByteLimit || total > ReactionPreviewByteLimit {
			return errors.New("reaction source byte budget exceeded")
		}
		date, err := netmail.ParseDate(item.Date)
		if item.GmailID == "" || strings.TrimSpace(item.GmailID) != item.GmailID || seen[item.GmailID] || item.Source.ExtID == "" || item.Precondition == "" || item.QuoteSightings < 0 || err != nil || date.Unix() != item.Source.TS {
			return errors.New("invalid reaction manifest item")
		}
		seen[item.GmailID] = true
		if item.Refusal != "" || !ReactionOnly(item.Inspection) || InspectionDigest(item.Inspection) != item.EvidenceDigest || strings.TrimSpace(item.Source.InReplyTo) != item.Inspection.TargetMessageID {
			return errors.New("refused or invalid reaction preview")
		}
	}
	return nil
}

// CheckReactionPreviewSources checks lengths in SQLite before loading bodies or
// opening the provider boundary. It never truncates a source into apparent proof.
func (s *Store) CheckReactionPreviewSources(accountID string, ids []string) error {
	if strings.TrimSpace(accountID) == "" || strings.TrimSpace(accountID) != accountID || len(ids) == 0 || len(ids) > ReactionPreviewLimit {
		return errors.New("preview requires an explicit account and 1..20 source IDs")
	}
	seen := map[string]bool{}
	var total int64
	for _, id := range ids {
		if id == "" || strings.TrimSpace(id) != id || seen[id] {
			return errors.New("preview IDs must be distinct and nonempty")
		}
		seen[id] = true
		var size int64
		err := s.db.QueryRow(`select length(cast(e.ext_id as blob))+length(cast(coalesce(m.message_id,'') as blob))+length(cast(coalesce(m.in_reply_to,'') as blob))+length(cast(coalesce(m.from_addr,'') as blob))+length(cast(coalesce(e.body_text,'') as blob))+length(cast(coalesce(e.body_html,'') as blob)) from gmail_copies c join entries e on e.id=c.entry_id join mail_detail m on m.entry_id=e.id where c.account_id=? and c.gmail_id=?`, accountID, id).Scan(&size)
		if err != nil {
			return err
		}
		total += size
		if size > ReactionSourceByteLimit || total > ReactionPreviewByteLimit {
			return errors.New("reaction source byte budget exceeded")
		}
	}
	return nil
}

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
	if err := s.CheckReactionPreviewSources(accountID, []string{gmailID}); err != nil {
		return item, err
	}
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

// PreviewReactionTargets overlays the complete bounded manifest's prospective
// sidecars, then uses the same resolver as the persisted views. No local write
// or unknown source-copy account is needed to project the post-apply result.
func (s *Store) PreviewReactionTargets(manifest *ReactionManifest) error {
	var prospective [][]any
	for _, item := range manifest.Items {
		if item.Refusal != "" {
			continue
		}
		if !ReactionOnly(item.Inspection) {
			return errors.New("invalid prospective reaction inspection")
		}
		var id int64
		if err := s.db.QueryRow(`select id from entries where ext_id=?`, item.Source.ExtID).Scan(&id); err != nil {
			return err
		}
		e, m := reactionItemEvidence(item)
		prospective = append(prospective, []any{item.Source.ExtID, id, reactionSnapshot(e, m, 0), manifest.AccountID, item.GmailID,
			reactionSemantic(true, item.Inspection.Candidates[0].Emoji, item.Inspection.TargetMessageID, e, m, 0), item.Inspection.Candidates[0].Emoji, item.Inspection.TargetMessageID})
	}
	if len(prospective) == 0 {
		return nil
	}
	raw, err := json.Marshal(prospective)
	if err != nil {
		return err
	}
	prefix := `with prospective as (
 select json_extract(value,'$[0]') as source_ext_id,json_extract(value,'$[1]') as entry_id,
 json_extract(value,'$[2]') as snapshot,json_extract(value,'$[3]') as account_id,json_extract(value,'$[4]') as gmail_id,
 json_extract(value,'$[5]') as semantic,json_extract(value,'$[6]') as emoji,json_extract(value,'$[7]') as target_ref from json_each(?)
 ), reaction_sources as (
 select * from main.reaction_sources where source_ext_id not in (select source_ext_id from prospective)
 union select source_ext_id,entry_id,snapshot from prospective
 ), reaction_inspections as (
 select account_id,gmail_id,source_ext_id,semantic,snapshot,candidate,emoji,target_ref from main.reaction_inspections
 union select account_id,gmail_id,source_ext_id,semantic,snapshot,1,emoji,target_ref from prospective
 ), sightings as (
 select * from main.sightings where seen_in is null or seen_in not in (select entry_id from prospective)
 ), reaction_agreed as (` + reactionAgreedSQL + `), reaction_resolved as (` + reactionResolvedSQL + `), reaction_active as (` + reactionActiveSQL + `) `
	targets, err := reviewedReactionTargets(s.db, prefix, []any{string(raw)}, *manifest)
	if err != nil {
		return err
	}
	for n := range manifest.Items {
		item := &manifest.Items[n]
		if item.Refusal == "" {
			item.TargetExtID = targets[item.Source.ExtID].target
			item.Status = targets[item.Source.ExtID].status
		}
	}
	return nil
}

type reviewedReactionTarget struct{ target, status string }

func reviewedReactionTargets(q corpusQuery, prefix string, args []any, manifest ReactionManifest) (map[string]reviewedReactionTarget, error) {
	var sources []string
	for _, item := range manifest.Items {
		sources = append(sources, item.Source.ExtID)
	}
	ph, sourceArgs := placeholders(sources)
	proof, proofArgs := activeReactionForAccountSQL("ra", manifest.AccountID)
	args = append(args, proofArgs...)
	args = append(args, sourceArgs...)
	rows, err := q.Query(prefix+`select e.ext_id,
 case when active.source_id is not null then t.ext_id else '' end,
 case when r.targets is null or r.targets!=1 then 'unresolved-missing-ambiguous-or-inaccessible'
 when r.target_id=r.source_id then 'unresolved-self'
 when exists(select 1 from reaction_agreed a where a.source_id=r.target_id) then 'unresolved-reaction-target'
 when active.source_id is not null then 'resolved'
 else 'unresolved-inaccessible' end
 from entries e left join (`+reactionResolutionSQL+`) r on r.source_id=e.id
 left join (select ra.* from reaction_active ra where `+proof+`) active on active.source_id=e.id
 left join entries t on t.id=active.target_id
 where e.ext_id in (`+ph+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]reviewedReactionTarget{}
	for rows.Next() {
		var source string
		var target reviewedReactionTarget
		if err = rows.Scan(&source, &target.target, &target.status); err != nil {
			return nil, err
		}
		out[source] = target
	}
	return out, rows.Err()
}

func reactionItemEvidence(item ReactionPreviewItem) (Entry, *Mail) {
	return Entry{ExtID: item.Source.ExtID, BodyText: item.Source.Body, BodyHTML: item.Source.BodyHTML, TS: time.Unix(item.Source.TS, 0)},
		&Mail{GmailID: item.GmailID, MessageID: item.Source.MessageID, InReplyTo: item.Source.InReplyTo, From: item.Source.From, SourceDate: item.Date, ReactionInspection: item.Inspection}
}

func InspectionDigest(r *mail.ReactionInspection) string {
	raw, _ := json.Marshal(r)
	return reactionDigest(raw)
}

// ApplyReactionManifest changes only sidecars and explicitly reviewed source-
// origin sightings with independent evidence. Every precondition is checked
// before any write, and the audit row makes exact replay a no-op.
func (s *Store) ApplyReactionManifest(manifest ReactionManifest) (bool, error) {
	if err := manifest.Validate(); err != nil {
		return false, err
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
		e, m := reactionItemEvidence(item)
		if err = putReactionEvidence(tx, manifest.AccountID, e, m, id); err != nil {
			return false, err
		}
	}
	targets, err := reviewedReactionTargets(tx, "", nil, manifest)
	if err != nil {
		return false, err
	}
	for _, item := range manifest.Items {
		actual, ok := targets[item.Source.ExtID]
		if !ok || actual.target != item.TargetExtID || actual.status != item.Status {
			return false, errors.New("reviewed reaction target/status differs from post-apply association")
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
