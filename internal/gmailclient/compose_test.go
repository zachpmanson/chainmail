package gmailclient

import (
	"errors"
	"strings"
	"testing"
)

func TestComposeRejectsMissingRequiredFieldsBeforeMailboxAccess(t *testing.T) {
	client := Client{}
	for _, tc := range []struct {
		name, to, subject, body, want string
	}{
		{name: "recipient", subject: "Hello", body: "Words", want: "to:"},
		{name: "subject", to: "reader@example.com", body: "Words", want: "subject:"},
		{name: "plain text body", to: "reader@example.com", subject: "Hello", want: "body:"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := client.Compose(tc.to, tc.subject, tc.body, true)
			if !errors.Is(err, ErrUnsent) {
				t.Fatalf("Compose() error = %v, want ErrUnsent", err)
			}
			if got := err.Error(); !strings.Contains(got, tc.want) {
				t.Fatalf("Compose() error = %q, want it to name %q", got, tc.want)
			}
		})
	}
}

func TestComposeRejectsInvalidRecipientBeforeMailboxAccess(t *testing.T) {
	_, err := (Client{}).Compose("not-an-address", "Hello", "Words", true)
	if !errors.Is(err, ErrUnsent) {
		t.Fatalf("Compose() error = %v, want ErrUnsent", err)
	}
}
