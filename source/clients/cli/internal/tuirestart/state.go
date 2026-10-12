// Package tuirestart transfers this terminal's state, never agent ownership.
package tuirestart

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const Protocol = "cercano-tui-handoff-1"
const MaxStateBytes = 1 << 20

var ErrState = errors.New("invalid TUI restart state")

type State struct {
	ConversationID   string `json:"conversation_id"`
	WorkingDirectory string `json:"working_directory"`
	Draft            string `json:"draft"`
}

func (s State) Validate() error {
	if !utf8.ValidString(s.Draft) || !utf8.ValidString(s.WorkingDirectory) || strings.ContainsAny(s.ConversationID, "\x00\r\n") || len(s.ConversationID) > 1024 || len(s.WorkingDirectory) > 4096 || len(s.Draft) > MaxStateBytes/2 || (s.WorkingDirectory != "" && !filepath.IsAbs(s.WorkingDirectory)) {
		return ErrState
	}
	return nil
}
func Encode(s State) ([]byte, error) {
	if e := s.Validate(); e != nil {
		return nil, e
	}
	b, e := json.Marshal(s)
	if e != nil || len(b) > MaxStateBytes {
		return nil, ErrState
	}
	return b, nil
}
func Decode(r io.Reader) (State, error) {
	var s State
	b, e := io.ReadAll(io.LimitReader(r, MaxStateBytes+1))
	if e != nil || len(b) > MaxStateBytes {
		return s, ErrState
	}
	if !utf8.Valid(b) {
		return s, ErrState
	}
	d := json.NewDecoder(bytes.NewReader(b))
	tok, e := d.Token()
	if e != nil || tok != json.Delim('{') {
		return s, ErrState
	}
	seen := map[string]bool{}
	for d.More() {
		tok, e = d.Token()
		if e != nil {
			return s, ErrState
		}
		key, ok := tok.(string)
		if !ok || seen[key] {
			return s, ErrState
		}
		seen[key] = true
		var raw json.RawMessage
		if e = d.Decode(&raw); e != nil || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return s, ErrState
		}
		switch key {
		case "conversation_id":
			e = json.Unmarshal(raw, &s.ConversationID)
		case "working_directory":
			e = json.Unmarshal(raw, &s.WorkingDirectory)
		case "draft":
			e = json.Unmarshal(raw, &s.Draft)
		default:
			return s, ErrState
		}
		if e != nil {
			return s, ErrState
		}
	}
	tok, e = d.Token()
	if e != nil || tok != json.Delim('}') || len(seen) != 3 {
		return s, ErrState
	}
	if _, e = d.Token(); e != io.EOF {
		return s, ErrState
	}
	return s, s.Validate()
}
