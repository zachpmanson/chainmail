package gmailclient

import (
	"bytes"
	"encoding/base64"
	"errors"
	"net/mail"
	"strings"

	"google.golang.org/api/googleapi"
)

type DraftSaveCheck struct {
	UnchangedCompatible bool   `json:"unchangedCompatible"`
	BodyEditCompatible  bool   `json:"bodyEditCompatible"`
	Context             string `json:"context"`
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
			return false
		}
		final, err := c.prepareDraftReplacement(raw, next)
		if err != nil {
			return false
		}
		return checkDraftReplacement(raw, final) == nil
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
