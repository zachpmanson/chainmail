package corpus

import (
	"strings"
	"time"

	"github.com/zachpmanson/chainmail/internal/textsim"
	"github.com/zachpmanson/chainmail/internal/unnest"
)

type forwardBody struct {
	words  []string
	header unnest.Recovered
}

// A named forward boundary, not merely a reply quoting the same parent. The
// forwarded text is evidence about the wrapper, not its sender's own words.
func bareForward(body, html string) *forwardBody {
	firstLine, _, _ := strings.Cut(strings.TrimSpace(body), "\n")
	if !strings.Contains(strings.ToLower(firstLine), "forwarded message") {
		return nil
	}
	blocks := unnest.Peel(body)
	if len(blocks) == 0 || blocks[0].Kind != unnest.KindForwardRule || blocks[0].Depth != 0 {
		return nil
	}
	firstLine, _, _ = strings.Cut(blocks[0].Sentinel, "\n")
	if !strings.Contains(strings.ToLower(firstLine), "forwarded message") {
		return nil
	}
	recovered := unnest.Parse(blocks[0])
	if recovered.Address == "" || recovered.Sent.IsZero() {
		return nil
	}
	words := copyWords(blocks[0].Text, "", true)
	if len(words) < minTwinTokens {
		return nil
	}
	// HTML reduction flattens line breaks, so it cannot establish a forward
	// boundary. Require all words, including header addresses and IDs, to agree:
	// prose reduction would erase contradictory identity evidence.
	if html != "" && !wordSlicesEqual(textsim.Tokens(body), textsim.Tokens(twinHTMLText(html))) {
		return nil
	}
	return &forwardBody{words: words, header: recovered}
}

// Read graph and recipients fresh, after closing the body scan. A cached body
// cannot vouch for a parent or routing evidence that has since changed.
func forwardEvidence(q corpusQuery, copies []twinCopy) error {
	for i := range copies {
		c := &copies[i]
		if c.parent == 0 || c.derived || (!c.empty && c.forward == nil) {
			continue
		}
		rows, err := q.Query(`select person_id from participants where entry_id=? and role=? order by person_id`, c.id, RoleTo)
		if err != nil {
			return err
		}
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			c.to = append(c.to, id)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		if c.quoted || c.forward == nil {
			continue
		}
		h := c.forward.header
		address, err := NormaliseIdentity(KindEmail, h.Address)
		if err != nil {
			return err
		}
		var body, html, subject, ext, messageID string
		var parent twinCopy
		var sender, nestedSender, ts int64
		if err := q.QueryRow(`
			select coalesce(e.body_text,''),coalesce(e.body_html,''),e.quoted,e.derived,
			       coalesce(e.person_id,0),e.ts,coalesce(e.subject,''),e.ext_id,
			       coalesce(m.message_id,''),
			       coalesce((select person_id from identities where kind=? and value=?),0)
			from entries e left join mail_detail m on m.entry_id=e.id where e.id=?`,
			KindEmail, address, c.parent).Scan(&body, &html, &parent.quoted, &parent.derived,
			&sender, &ts, &subject, &ext, &messageID, &nestedSender); err != nil {
			return err
		}
		parent.wall = time.Unix(ts, 0).UTC()
		_, clockOK := twinOffset(twinCopy{wall: h.Sent, quoted: true}, parent)
		// A reply edge can come from In-Reply-To rather than this forward. Its
		// prose alone cannot vouch for contradictory nested headers.
		idOK := h.MessageID == "" || h.MessageID == strings.Trim(messageID, "<>") ||
			(strings.HasPrefix(ext, "mail:") && h.MessageID == strings.Trim(strings.TrimPrefix(ext, "mail:"), "<>"))
		if parent.derived || sender == 0 || nestedSender != sender || !idOK ||
			(h.Subject != "" && subject != "" && subjectKey(h.Subject) != subjectKey(subject)) ||
			!clockOK || h.Sent.Sub(parent.wall).Abs() > 26*time.Hour ||
			!wordSlicesEqual(c.forward.words, copyWords(body, html, parent.quoted)) {
			c.forward = nil
		}
	}
	return nil
}

// twinnable has already checked the clock; its callers partition by author.
func sameEmptyForward(a, b twinCopy) bool {
	q, m := a, b
	if !a.quoted && b.quoted {
		q, m = b, a
	}
	subject := subjectKey(q.subject)
	return q.quoted && !m.quoted && q.empty && !q.derived && !m.derived &&
		q.parent != 0 && q.parent == m.parent && m.forward != nil && len(m.forward.words) >= minTwinTokens &&
		q.container != "" && q.container == m.container &&
		subject != "" && subject == subjectKey(m.subject) &&
		sharedRecipient(q.to, m.to)
}

func sharedRecipient(a, b []int64) bool {
	for _, x := range a {
		for _, y := range b {
			if x == y {
				return true
			}
		}
	}
	return false
}
