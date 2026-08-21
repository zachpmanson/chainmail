// Package spec turns corpus rows into a timeline spec, the contract the
// renderer consumes (schema/timeline.schema.json).
//
// The division of labour is deliberate: everything here is a join, not a
// judgement. Dates, zones, senders, recipients, subjects, reply edges,
// attachments and the participant cast all follow mechanically from what was
// ingested, so they are produced here, and so is `body` — the message's own text
// rendered as presentation HTML (body.go), which is a conversion rather than a
// judgement. The fields that need a human or a model — `openItems`, cross-links,
// the editorial `subtitle`, any gloss written *about* a message — are left empty
// on purpose, so that a later pass has a narrow, well-defined job rather than a
// blank page.
//
// Selection is also out of scope: Generate is told which entries to render (see
// Options) and expands only the reply-graph closure of that set.
package spec

// Spec is one timeline. Field order follows the schema; every optional field is
// omitempty so a generated spec carries only what the corpus actually knows.
type Spec struct {
	SpecVersion  int               `json:"specVersion,omitempty"`
	Title        string            `json:"title"`
	Subtitle     string            `json:"subtitle,omitempty"`
	RunLabel     string            `json:"runLabel,omitempty"`
	Runs         []string          `json:"runs,omitempty"`
	Theme        string            `json:"theme,omitempty"`
	OpenItems    []string          `json:"openItems,omitempty"`
	Avatars      map[string]string `json:"avatars,omitempty"`
	Participants []Participant     `json:"participants,omitempty"`
	Queries      []Query           `json:"queries,omitempty"`
	Threads      []Thread          `json:"threads,omitempty"`
	SourceNotes  []SourceNote      `json:"sourceNotes,omitempty"`
	Messages     []Entry           `json:"messages"`
}

// Participant is one member of the cast, including people who only ever appear
// as recipients.
type Participant struct {
	Name  string `json:"name"`
	Org   string `json:"org,omitempty"`
	Email string `json:"email,omitempty"`
	Role  string `json:"role,omitempty"`
	Note  string `json:"note,omitempty"`
}

// Query records a search that was run, so a null result is interpretable.
type Query struct {
	Q    string `json:"q"`
	Note string `json:"note,omitempty"`
}

// Thread is a mail thread the transcript was assembled from.
type Thread struct {
	Subject string `json:"subject,omitempty"`
	ID      string `json:"id,omitempty"`
	Count   int    `json:"count,omitempty"`
	Span    string `json:"span,omitempty"`
	Note    string `json:"note,omitempty"`
}

// SourceNote is a provenance caveat: what could not be recovered, and why.
type SourceNote struct {
	Title string   `json:"title"`
	Items []string `json:"items"`
}

// Entry is one message or note in the transcript.
type Entry struct {
	Kind        string       `json:"kind,omitempty"`
	ID          string       `json:"id,omitempty"`
	Date        string       `json:"date"`
	Time        string       `json:"time,omitempty"`
	TZ          string       `json:"tz,omitempty"`
	TZSource    string       `json:"tzSource,omitempty"`
	Sender      string       `json:"sender,omitempty"`
	Org         string       `json:"org,omitempty"`
	FromEmail   string       `json:"fromEmail,omitempty"`
	To          string       `json:"to,omitempty"`
	Subject     string       `json:"subject,omitempty"`
	Label       string       `json:"label,omitempty"`
	Body        string       `json:"body"`
	Quoted      bool         `json:"quoted,omitempty"`
	Me          bool         `json:"me,omitempty"`
	Source      string       `json:"source,omitempty"`
	GmailID     string       `json:"gmailId,omitempty"`
	ThreadID    string       `json:"threadId,omitempty"`
	Parent      string       `json:"parent,omitempty"`
	Meta        bool         `json:"meta,omitempty"`
	Mentions    []string     `json:"mentions,omitempty"`
	Attachments []Attachment `json:"attachments,omitempty"`
}

// Attachment is what the renderer shows as a chip under a message.
type Attachment struct {
	Name    string `json:"name"`
	Kind    string `json:"kind,omitempty"`
	Size    string `json:"size,omitempty"`
	GmailID string `json:"gmailId,omitempty"`
	// Link opens the attachment at its source, for attachments not reached
	// through Gmail. Without it a Slack attachment is an unopenable label.
	Link string `json:"link,omitempty"`
	// Preview is a thumbnail as a data: URI, present only where the archive kept
	// the bytes and the picture is content rather than decoration. Never a URL:
	// the page must render without a network. See preview.go.
	Preview string `json:"preview,omitempty"`
	// PreviewW and PreviewH are the thumbnail's own pixel size, so the page
	// reserves the right space before the image decodes and the transcript does
	// not reflow as it reads.
	PreviewW int `json:"previewW,omitempty"`
	PreviewH int `json:"previewH,omitempty"`
}
