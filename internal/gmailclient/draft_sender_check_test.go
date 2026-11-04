package gmailclient

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestReadOnlyDraftSenderDiagnosis(t *testing.T) {
	const raw = "From: Synthetic Owner <owner@example.test>\r\nContent-Type: text/plain\r\n\r\nSECRET BODY"
	for _, tc := range []struct {
		name, reason string
		change       func(*metadataFixture)
	}{
		{name: "matching sender"},
		{name: "profile unavailable", reason: "sender_identity_unverified", change: func(f *metadataFixture) { f.failProfile = true }},
		{name: "settings unavailable", reason: "sender_identity_unverified", change: func(f *metadataFixture) { f.failSendAs = true }},
		{name: "ambiguous identities", reason: "sender_identity_unverified", change: func(f *metadataFixture) { f.sendAs = append(f.sendAs, f.sendAs[0]) }},
		{name: "default flag", reason: "sender_not_default", change: func(f *metadataFixture) { f.sendAs[0].IsDefault = false }},
		{name: "primary flag", reason: "sender_not_primary", change: func(f *metadataFixture) { f.sendAs[0].IsPrimary = false }},
		{name: "profile settings mismatch", reason: "sender_profile_mismatch", change: func(f *metadataFixture) { f.sendAs[0].SendAsEmail = "other@example.test" }},
		{name: "draft address mismatch", reason: "sender_address_mismatch", change: func(f *metadataFixture) {
			f.profile.EmailAddress = "other@example.test"
			f.sendAs[0].SendAsEmail = "other@example.test"
		}},
		{name: "different configured name", change: func(f *metadataFixture) { f.sendAs[0].DisplayName = "Different Presentation" }},
		{name: "empty configured name", change: func(f *metadataFixture) { f.sendAs[0].DisplayName = "" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, c := newMetadataFixture(t, base64.RawURLEncoding.EncodeToString([]byte(raw)))
			if tc.change != nil {
				tc.change(f)
			}
			err := c.CheckDraftSender("d1")
			if tc.reason == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else {
				assertMetadataRefusal(t, err, f)
				if !strings.Contains(err.Error(), "reason="+tc.reason+";") {
					t.Fatalf("unexpected category: %v", err)
				}
			}
			if f.writes != 0 {
				t.Fatalf("diagnosis mutated Gmail: %d", f.writes)
			}
			for path, count := range f.reads {
				if count != 1 || (path != "/gmail/v1/users/me/drafts/d1" && path != "/gmail/v1/users/me/profile" && path != "/gmail/v1/users/me/settings/sendAs") {
					t.Fatalf("unexpected provider read: %s count=%d", path, count)
				}
			}
		})
	}
}

func TestReadOnlyDraftSenderDiagnosisDeletedDraft(t *testing.T) {
	f, c := newMetadataFixture(t, "")
	f.deleted = true
	err := c.CheckDraftSender("d1")
	if !IsNotFound(err) || strings.Contains(err.Error(), "PRIVATE") || strings.Contains(err.Error(), "owner@example.test") || f.writes != 0 {
		t.Fatalf("deleted draft diagnosis: %v; mutations=%d", err, f.writes)
	}
}

func TestReadOnlyDraftSenderDiagnosisMalformedHeaders(t *testing.T) {
	for _, raw := range []string{
		"From: Synthetic Owner <owner@example.test>\r\nFrom: alias@example.test\r\n\r\nSECRET BODY",
		"From: PRIVATE MALFORMED FROM\r\n\r\nSECRET BODY",
		"From:\r\n\r\nSECRET BODY",
	} {
		f, c := newMetadataFixture(t, base64.RawURLEncoding.EncodeToString([]byte(raw)))
		assertMetadataRefusal(t, c.CheckDraftSender("d1"), f)
	}
}
