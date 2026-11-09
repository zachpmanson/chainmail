package mailingest

import (
	"fmt"

	"github.com/zachpmanson/chainmail/internal/corpus"
)

// PreviewReactions reads only these existing account-local sources, at most 20.
// It does not invoke ingest, Put, history, search, labels or repair.
func PreviewReactions(store *corpus.Store, mailbox Mailbox, accountID string, ids []string) (corpus.ReactionManifest, error) {
	manifest := corpus.ReactionManifest{Version: 1, AccountID: accountID, Items: []corpus.ReactionPreviewItem{}}
	if err := store.CheckReactionPreviewSources(accountID, ids); err != nil {
		return manifest, err
	}
	for _, id := range ids {
		item, err := store.PreviewReactionSource(accountID, id)
		if err != nil {
			return manifest, err
		}
		manifest.Items = append(manifest.Items, item)
	}
	for n := range manifest.Items {
		item := &manifest.Items[n]
		msg, err := mailbox.Read(item.GmailID)
		if err != nil {
			return manifest, fmt.Errorf("reading explicit reaction source: %w", err)
		}
		item.Inspection = msg.ReactionInspection
		item.Date = msg.Date
		item.EvidenceDigest = corpus.InspectionDigest(msg.ReactionInspection)
		ts, _, _ := parseDate(msg.Date)
		switch {
		case msg.ID != item.GmailID || msg.MessageID != item.Source.MessageID || msg.From != item.Source.From || msg.InReplyTo != item.Source.InReplyTo || msg.Body != item.Source.Body || msg.BodyHTML != item.Source.BodyHTML || ts.Unix() != item.Source.TS:
			item.Refusal = "source read differs from stored notification; no source rewrite permitted"
		case msg.Truncated || msg.HTMLTruncated || isDraft(msg.Labels):
			item.Refusal = "incomplete or draft source"
		case !reactionOnly(msg):
			item.Refusal = "not a supported independently validated reaction-only inspection"
		}
		if item.Refusal != "" {
			item.Status = "ordinary-refused"
			continue
		}
		if err = store.PreviewReactionAgreement(*item); err != nil {
			item.Refusal = err.Error()
			item.Status = "ordinary-conflict"
			continue
		}
	}
	err := store.PreviewReactionTargets(&manifest)
	return manifest, err
}
