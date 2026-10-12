package tuirestart

import (
	"bytes"
	"strings"
	"testing"
)

func TestStateRoundtrip(t *testing.T) {
	s := State{ConversationID: "conversation-123", WorkingDirectory: t.TempDir(), Draft: "unsent\nα draft"}
	b, e := Encode(s)
	if e != nil {
		t.Fatal(e)
	}
	got, e := Decode(bytes.NewReader(b))
	if e != nil || got != s {
		t.Fatal(got, e)
	}
}
func TestStateRefusesMalformed(t *testing.T) {
	for _, s := range []string{`{}`, `{"conversation_id":"","working_directory":"","draft":null}`, `{"conversation_id":"","working_directory":"","draft":"","draft":"other"}`, `{"draft":4}`, `{"extra":true}`, `{} {}`, strings.Repeat("x", MaxStateBytes+1)} {
		if _, e := Decode(strings.NewReader(s)); e == nil {
			t.Fatal("accepted malformed state")
		}
	}
	if _, e := Encode(State{WorkingDirectory: "relative"}); e == nil {
		t.Fatal("relative directory accepted")
	}
}
