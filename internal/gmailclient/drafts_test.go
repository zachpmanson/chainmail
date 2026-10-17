package gmailclient

import (
	"net/mail"
	"strings"
	"testing"
)

func TestDraftMIMEAndParseRoundTrip(t *testing.T) {
	want := Draft{
		To:      `"Ada Lovelace" <ada@example.test>`,
		Cc:      "<bo@example.test>",
		Subject: "A subject with café",
		Body:    "First line\nSecond line with café\n",
	}
	raw, err := draftMIME(want)
	if err != nil {
		t.Fatal(err)
	}
	msg, err := mail.ReadMessage(strings.NewReader(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	if msg.Header.Get("Content-Transfer-Encoding") != "quoted-printable" {
		t.Fatalf("encoding = %q", msg.Header.Get("Content-Transfer-Encoding"))
	}
	got, err := parseDraft(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got.To != want.To || got.Cc != want.Cc || got.Subject != want.Subject || got.Body != want.Body {
		t.Fatalf("round trip = %#v, want %#v", got, want)
	}
}

func TestDraftMIMERejectsInvalidRecipients(t *testing.T) {
	_, err := draftMIME(Draft{To: "not an address", Subject: "subject", Body: "body"})
	if err == nil || !strings.Contains(err.Error(), "invalid To recipients") {
		t.Fatalf("error = %v, want invalid To recipients", err)
	}
}

func TestDraftMIMERejectsHeaderInjection(t *testing.T) {
	_, err := draftMIME(Draft{To: "ada@example.test\r\nBcc: evil@example.test", Subject: "subject", Body: "body"})
	if err == nil {
		t.Fatal("accepted a recipient header with an injected Bcc")
	}
}

func TestAddHeader(t *testing.T) {
	got := string(addHeader([]byte("To: a@example.test\r\n\r\nbody"), "In-Reply-To", "<parent@example.test>"))
	if !strings.Contains(got, "To: a@example.test\r\nIn-Reply-To: <parent@example.test>\r\n\r\nbody") {
		t.Fatalf("header insertion = %q", got)
	}
}
