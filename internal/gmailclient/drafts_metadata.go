package gmailclient

import (
	"bytes"
	"net/mail"
	"strings"
)

// Copy only immutable, non-editor metadata. All other fields still have to
// survive the final replacement guard; this is not a general header copier.
func (c Client) preserveDraftMetadata(current, replacement []byte) ([]byte, error) {
	blocks, err := draftMetadataBlocks(current)
	if err != nil {
		return nil, err
	}
	msg, err := mail.ReadMessage(bytes.NewReader(current))
	if err != nil {
		return nil, draftRefusal("malformed_message")
	}
	if from := msg.Header.Get("From"); from != "" {
		if err := c.verifyDraftSender(from); err != nil {
			return nil, err
		}
	}
	return append(blocks, replacement...), nil
}

// Restrict preservation to well-formed CRLF headers. Refuse unusual line endings
// rather than normalizing immutable bytes (the body is not inspected here).
func draftMetadataBlocks(raw []byte) ([]byte, error) {
	end := bytes.Index(raw, []byte("\r\n\r\n"))
	if end < 0 {
		return nil, draftRefusal("malformed_message")
	}
	header := string(raw[:end])
	if strings.ContainsAny(strings.ReplaceAll(header, "\r\n", ""), "\r\n") {
		return nil, draftRefusal("malformed_message")
	}
	var blocks []string
	for _, line := range strings.Split(header, "\r\n") {
		for _, ch := range line {
			if ch == 127 || (ch < 32 && ch != '\t') {
				return nil, draftRefusal("malformed_message")
			}
		}
		if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
			if len(blocks) == 0 {
				return nil, draftRefusal("malformed_message")
			}
			blocks[len(blocks)-1] += line + "\r\n"
			continue
		}
		name, _, ok := strings.Cut(line, ":")
		if !ok || name == "" {
			return nil, draftRefusal("malformed_message")
		}
		for _, ch := range name {
			if ch < 33 || ch > 126 {
				return nil, draftRefusal("malformed_message")
			}
		}
		blocks = append(blocks, line+"\r\n")
	}
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return nil, draftRefusal("malformed_message")
	}
	for _, name := range []string{"Date", "Message-Id", "From"} {
		if len(msg.Header[name]) > 1 {
			return nil, draftRefusal("duplicate_header", name)
		}
	}
	var out []byte
	for _, block := range blocks {
		name, _, _ := strings.Cut(block, ":")
		switch strings.ToLower(name) {
		case "date", "message-id", "received", "from":
			value := msg.Header.Get(name)
			if strings.TrimSpace(block[len(name)+1:]) == "" {
				return nil, draftRefusal("malformed_metadata", name)
			}
			switch strings.ToLower(name) {
			case "date":
				if _, err := mail.ParseDate(value); err != nil {
					return nil, draftRefusal("malformed_metadata", name)
				}
			case "message-id":
				if !plainDraftMessageID(value) {
					return nil, draftRefusal("malformed_metadata", name)
				}
			}
			out = append(out, block...)
		}
	}
	return out, nil
}

// Deliberately support only the ordinary unquoted Message-ID form. Opaque but
// unusual RFC syntax is refused, never repaired or dropped.
func plainDraftMessageID(value string) bool {
	if len(value) < 5 || value[0] != '<' || value[len(value)-1] != '>' {
		return false
	}
	local, domain, ok := strings.Cut(value[1:len(value)-1], "@")
	if !ok || local == "" || domain == "" {
		return false
	}
	return plainDraftIDPart(local) && plainDraftIDPart(domain)
}

func plainDraftIDPart(value string) bool {
	for _, atom := range strings.Split(value, ".") {
		if atom == "" {
			return false
		}
		for _, ch := range atom {
			if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') ||
				(ch >= '0' && ch <= '9') || strings.ContainsRune("!#$%&'*+-/=?^_`{|}~", ch) {
				continue
			}
			return false
		}
	}
	return true
}

func (c Client) verifyDraftSender(value string) error {
	from, err := mail.ParseAddress(value)
	if err != nil || !strings.Contains(from.Address, "@") {
		return draftRefusal("sender_invalid", "From")
	}
	profile, err := c.svc.Users.GetProfile("me").Fields("emailAddress").Context(c.ctx).Do()
	if err != nil || profile == nil || !bareDraftAddress(profile.EmailAddress) {
		// Provider errors can contain private response data.
		return draftRefusal("sender_identity_unverified", "From")
	}
	settings, err := c.svc.Users.Settings.SendAs.List("me").Fields("sendAs(sendAsEmail,displayName,isPrimary,isDefault)").Context(c.ctx).Do()
	if err != nil || settings == nil {
		return draftRefusal("sender_identity_unverified", "From")
	}
	// No alternate sender may exist in the observed settings. This verifies a
	// snapshot, not provider behavior on a later send omitting From or an atomic
	// guarantee against external settings changes.
	if len(settings.SendAs) != 1 || settings.SendAs[0] == nil {
		return draftRefusal("sender_identity_unverified", "From")
	}
	sender := settings.SendAs[0]
	switch {
	case !sender.IsPrimary:
		return draftRefusal("sender_not_primary", "From")
	case !sender.IsDefault:
		return draftRefusal("sender_not_default", "From")
	case !bareDraftAddress(sender.SendAsEmail):
		return draftRefusal("sender_settings_address_invalid", "From")
	case !strings.EqualFold(profile.EmailAddress, sender.SendAsEmail):
		return draftRefusal("sender_profile_mismatch", "From")
	case !strings.EqualFold(from.Address, profile.EmailAddress):
		return draftRefusal("sender_address_mismatch", "From")
	case from.Name != sender.DisplayName && sender.DisplayName == "":
		return draftRefusal("sender_display_name_unconfigured", "From")
	case from.Name != sender.DisplayName:
		return draftRefusal("sender_display_name_mismatch", "From")
	}
	return nil
}

func bareDraftAddress(value string) bool {
	parsed, err := mail.ParseAddress(value)
	return err == nil && parsed.Name == "" && parsed.Address == value && strings.Contains(value, "@")
}
