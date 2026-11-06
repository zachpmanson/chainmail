package gmailclient

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestDraftSaveFailureCodePlainFormat(t *testing.T) {
	for _, tc := range []struct{ parameter, code string }{
		{"flowed", "alternative_plain_flowed"}, {"fixed", "alternative_plain_fixed"}, {"PRIVATE", "alternative_plain_parameters"},
		{"flowed; delsp=yes", "alternative_plain_flowed_delsp_yes"},
		{"flowed; delsp=no", "alternative_plain_flowed_delsp_no"},
		{"flowed; delsp=PRIVATE", "alternative_plain_flowed_delsp_other"},
		{"flowed; PRIVATE=value", "alternative_plain_flowed_extra"},
	} {
		raw := bytes.Replace(alternativeFixture(), []byte("text/plain; charset=UTF-8"), []byte("text/plain; charset=UTF-8; format="+tc.parameter), 1)
		_, err := parseAlternative(raw)
		if got := draftSaveFailureCode(err); got != tc.code {
			t.Fatalf("unsafe/wrong format category: %s", got)
		}
	}
}

func TestDraftSaveFailureCodeEmptySecondPart(t *testing.T) {
	raw := alternativeFixture()
	at := bytes.Index(raw, []byte("--abc123\r\nContent-Type: text/html"))
	if at < 0 {
		t.Fatal("fixture shape")
	}
	raw = append(raw[:at], []byte("--abc123\r\n--abc123--\r\n")...)
	_, err := parseAlternative(raw)
	if got := draftSaveFailureCode(err); got != "alternative_framing" {
		t.Fatalf("wrong parser stage: %s", got)
	}
}

func TestDraftSaveFailureCodesAreClosed(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{
		{draftRefusal("alternative_plain_lines"), "alternative_plain_lines"},
		{draftRefusal("alternative_framing"), "alternative_framing"},
		{draftRefusal("html_grammar"), "html_grammar"},
		{fmt.Errorf("PRIVATE: %w [reason=header_missing_or_changed; headers=PRIVATE]", ErrUnsupportedDraft), "header_missing_or_changed"},
		{fmt.Errorf("%w [reason=PRIVATE]", ErrUnsupportedDraft), "unsupported"},
		{errors.New("PRIVATE [reason=html_grammar]"), "unavailable"},
	} {
		got := draftSaveFailureCode(tc.err)
		if got != tc.want || strings.Contains(got, "PRIVATE") {
			t.Fatalf("unsafe failure code: %q", got)
		}
	}
}
