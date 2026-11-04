package gmailclient

import (
	"bytes"
	"errors"
	"fmt"
	"net/mail"
	"strings"

	"google.golang.org/api/googleapi"
)

// CheckDraftSender runs only the update's metadata/sender checks, without
// constructing or saving a replacement. Success is not a MIME compatibility
// check or permission to send; Gmail can change after this snapshot.
func (c Client) CheckDraftSender(id string) error {
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("draft id is required")
	}
	got, err := c.svc.Users.Drafts.Get("me", id).Format("raw").Fields("message/raw").Context(c.ctx).Do()
	if err != nil {
		if IsNotFound(err) {
			return &googleapi.Error{Code: 404, Message: "Gmail draft no longer exists"}
		}
		return errors.New("Gmail could not read the draft for sender checking")
	}
	if got == nil || got.Message == nil {
		return draftRefusal("malformed_message")
	}
	raw, err := decodeDraftRaw(got.Message.Raw)
	if err != nil {
		return draftRefusal("malformed_message")
	}
	if _, err := draftMetadataBlocks(raw); err != nil {
		return err
	}
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return draftRefusal("malformed_message")
	}
	if from := msg.Header.Get("From"); from != "" {
		return c.verifyDraftSender(from)
	}
	return nil
}
