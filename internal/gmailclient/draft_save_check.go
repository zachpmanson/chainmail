package gmailclient

import (
	"bytes"
	"encoding/base64"
	"errors"
	"log"
	"net/mail"
	"strings"

	"google.golang.org/api/googleapi"
)

// Classify only fixed application reasons; never log error text or identifiers.
func draftSaveFailureCode(err error) string {
	if !errors.Is(err, ErrUnsupportedDraft) {
		return "unavailable"
	}
	for _, code := range []string{
		"alternative_size", "alternative_root_type", "alternative_boundary", "alternative_version",
		"alternative_framing", "alternative_part_headers", "alternative_part_type", "alternative_part_pair",
		"alternative_plain_flowed", "alternative_plain_flowed_delsp_yes", "alternative_plain_flowed_delsp_no",
		"alternative_plain_flowed_delsp_other", "alternative_plain_flowed_extra",
		"alternative_plain_fixed", "alternative_plain_parameters", "alternative_html_parameters",
		"alternative_plain_lines", "alternative_plain_encoding", "alternative_plain_text",
		"alternative_html_lines", "alternative_html_encoding", "alternative_text_mismatch", "html_grammar",
		"malformed_message", "duplicate_header", "header_missing_or_changed", "body_text",
		"replacement_body", "boundary_collision", "immutable_mime_changed",
	} {
		if strings.Contains(err.Error(), "[reason="+code+"]") || strings.Contains(err.Error(), "[reason="+code+";") {
			return code
		}
	}
	return "unsupported"
}

type DraftSaveCheck struct {
	UnchangedCompatible bool   `json:"unchangedCompatible"`
	BodyEditCompatible  bool   `json:"bodyEditCompatible"`
	Context             string `json:"context"`
	Status              string `json:"status,omitempty"`
}

// CheckDraftSave constructs real replacements, but performs only GETs. This is
// snapshot/representative evidence, not an atomic promise about a future save.
func (c Client) CheckDraftSave(id string) (DraftSaveCheck, error) {
	out := DraftSaveCheck{Context: "unsupported"}
	if strings.TrimSpace(id) == "" {
		return out, errors.New("Gmail could not check draft save compatibility")
	}
	got, err := c.svc.Users.Drafts.Get("me", id).Format("raw").Context(c.ctx).Do()
	if err != nil {
		if IsNotFound(err) {
			return out, &googleapi.Error{Code: 404, Message: "Gmail draft no longer exists"}
		}
		return out, errors.New("Gmail could not check draft save compatibility")
	}
	if got == nil || got.Message == nil || got.Message.Id == "" || len(got.Message.Raw) > base64.URLEncoding.EncodedLen(maxDraftMIMEBytes) {
		return out, nil
	}
	raw, err := decodeDraftRaw(got.Message.Raw)
	if err != nil || len(raw) > maxDraftMIMEBytes {
		return out, nil
	}
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return out, nil
	}
	// Even an empty reply field is not evidence that parent checks are unnecessary.
	for _, key := range []string{"In-Reply-To", "References"} {
		if _, ok := msg.Header[key]; ok {
			out.Context = "reply_context"
			return out, nil
		}
	}
	d, err := parseDraft(raw)
	if err != nil {
		return out, nil
	}
	prepare := func(d Draft) bool {
		next, err := draftReplacementMIME(d, raw)
		if err != nil {
			log.Printf("draft save-check refused: code=%s", draftSaveFailureCode(err))
			return false
		}
		final, err := c.prepareDraftReplacement(raw, next)
		if err != nil {
			log.Printf("draft save-check refused: code=%s", draftSaveFailureCode(err))
			return false
		}
		if err := checkDraftReplacement(raw, final); err != nil {
			log.Printf("draft save-check refused: code=%s", draftSaveFailureCode(err))
			return false
		}
		return true
	}
	out.UnchangedCompatible = prepare(d)
	if !out.UnchangedCompatible {
		return out, nil
	}
	out.Context = "compose"
	// Exercise Unicode, entities, repeated spaces, tabs, CRLF and blank lines.
	d.Body = "Chainmail save-check & < > café 😀\r\n\r\n spaced  text\tNBSP:\u00a0\r\n"
	out.BodyEditCompatible = prepare(d)
	return out, nil
}

// CheckDraftSaveWithContext checks a revision-bound, server-planned replacement.
// Parent resolution and Reply planning belong to the selected-account caller.
func (c Client) CheckDraftSaveWithContext(d Draft, parentID string) (DraftSaveCheck, error) {
	out := DraftSaveCheck{Context: "unsupported", Status: "indeterminate"}
	if strings.TrimSpace(d.ID) == "" || strings.TrimSpace(d.MessageID) == "" {
		return out, nil
	}
	got, err := c.svc.Users.Drafts.Get("me", d.ID).Format("raw").Context(c.ctx).Do()
	if err != nil {
		if IsNotFound(err) {
			return out, &googleapi.Error{Code: 404, Message: "Gmail draft no longer exists"}
		}
		return out, errors.New("Gmail could not check draft save compatibility")
	}
	if got == nil || got.Message == nil || got.Message.Id == "" {
		return out, nil
	}
	if got.Message.Id != d.MessageID {
		out.Status = "revision_changed"
		return out, nil
	}
	if len(got.Message.Raw) > base64.URLEncoding.EncodedLen(maxDraftMIMEBytes) {
		out.Status = "unsupported"
		return out, nil
	}
	raw, err := decodeDraftRaw(got.Message.Raw)
	if err != nil || len(raw) > maxDraftMIMEBytes {
		out.Status = "unsupported"
		return out, nil
	}
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		out.Status = "unsupported"
		return out, nil
	}
	_, resolution := replyHeaders(msg.Header).Parent()
	context := "compose"
	switch resolution {
	case "compose":
		if parentID != "" {
			return out, nil
		}
	case "candidate":
		if parentID == "" {
			return out, nil
		}
		context = "reply"
	default:
		out.Status = "unsupported"
		return out, nil
	}
	for _, body := range []string{d.Body, "Chainmail save-check & < > café 😀\r\n\r\n spaced  text\tNBSP:\u00a0\r\n"} {
		d.Body = body
		if _, _, err := c.prepareDraftUpdate(d, parentID, got, raw); err != nil {
			out.Status = draftSaveCheckStatus(err)
			return out, nil
		}
	}
	out.Context, out.Status = context, "supported"
	out.UnchangedCompatible, out.BodyEditCompatible = true, true
	return out, nil
}

func draftSaveCheckStatus(err error) string {
	if IsConflict(err) {
		return "revision_changed"
	}
	if !errors.Is(err, ErrUnsupportedDraft) {
		return "indeterminate"
	}
	// Only known definitive refusals are unsupported. Sender reads also wrap
	// ErrUnsupportedDraft; unavailable or unknown evidence stays indeterminate.
	if code := draftSaveFailureCode(err); code != "unsupported" && code != "unavailable" {
		return "unsupported"
	}
	for _, reason := range []string{
		"malformed_metadata", "media_type", "mime_parameter", "charset", "transfer_encoding",
		"thread_mismatch", "parent_metadata_invalid", "sender_invalid", "sender_address_mismatch", "sender_profile_mismatch",
	} {
		if strings.Contains(err.Error(), "[reason="+reason+"]") || strings.Contains(err.Error(), "[reason="+reason+";") {
			return "unsupported"
		}
	}
	return "indeterminate"
}
