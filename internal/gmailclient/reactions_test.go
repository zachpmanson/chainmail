package gmailclient

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/zachpmanson/chainmail/internal/mailingest"
	"github.com/zachpmanson/docket/gmail/mail"
)

func TestReactionInspectionSurvivesLibraryAndCLIConversion(t *testing.T) {
	raw, err := os.ReadFile("../../testdata/gmail-reaction-synthetic.json")
	if err != nil {
		t.Fatal(err)
	}
	var library mail.Message
	var cli mailingest.Message
	if err = json.Unmarshal(raw, &library); err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &cli); err != nil {
		t.Fatal(err)
	}
	converted := convertMessage(&library)
	if !reflect.DeepEqual(converted.ReactionInspection, cli.ReactionInspection) || converted.Body != cli.Body || converted.BodyHTML != cli.BodyHTML || converted.MessageID != cli.MessageID || converted.InReplyTo != cli.InReplyTo {
		t.Fatal("library/CLI reaction evidence parity lost")
	}
	if err = mail.ValidateReactionInspection(converted.ReactionInspection); err != nil {
		t.Fatal(err)
	}
	converted.ReactionInspection = nil
	library.ReactionInspection = nil
	if convertMessage(&library).ReactionInspection != nil {
		t.Fatal("old decoder was claimed inspected")
	}
}
