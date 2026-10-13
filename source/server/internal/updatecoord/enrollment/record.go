// Package enrollment handles installer-owned identity records for explicitly
// enrolled per-user self-managed installations. It never infers package ownership
// or enrolls on read. Callers must independently establish installation authority;
// this record cannot override package-manager evidence or authorize elevation.
package enrollment

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"runtime"
	"unicode/utf8"
)

const FileName = ".cercano-install.json"
const MaxRecordBytes = 8192

var ErrRecord = errors.New("enrollment: invalid installation identity record")
var ErrExists = errors.New("enrollment: identity record already exists")
var ErrUnsafe = errors.New("enrollment: unsafe installation or record")
var ErrCommitted = errors.New("enrollment: record published but durability uncertain; read existing record before retry")

// Record is stable across version changes. It contains no credentials, arbitrary
// commands or updater-state path. Root and launcher names bind local discovery;
// the ID is random rather than derived from an executable or directory name.
type Record struct {
	SchemaVersion int    `json:"schema_version"`
	InstallID     string `json:"install_id"`
	Root          string `json:"root"`
	Scope         string `json:"scope"`
	GOOS          string `json:"goos"`
	Agent         string `json:"agent"`
	Client        string `json:"client"`
}

func names() (string, string) {
	if runtime.GOOS == "windows" {
		return "cercano.exe", "cercano-cli.exe"
	}
	return "cercano", "cercano-cli"
}
func (r Record) Validate(root string) error {
	a, c := names()
	if r.SchemaVersion != 1 || r.Scope != "user" || r.GOOS != runtime.GOOS || r.Root != root || !filepath.IsAbs(root) || filepath.Clean(root) != root || !utf8.ValidString(root) || len(root) > 4096 || r.Agent != a || r.Client != c || len(r.InstallID) != 32 {
		return ErrRecord
	}
	for _, c := range r.InstallID {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return ErrRecord
		}
	}
	return nil
}
func fresh(root string) (Record, error) {
	var id [16]byte
	if _, e := rand.Read(id[:]); e != nil {
		return Record{}, e
	}
	a, c := names()
	r := Record{SchemaVersion: 1, InstallID: hex.EncodeToString(id[:]), Root: root, Scope: "user", GOOS: runtime.GOOS, Agent: a, Client: c}
	return r, r.Validate(root)
}
func decode(data []byte, root string) (Record, error) {
	var r Record
	if len(data) > MaxRecordBytes || !utf8.Valid(data) {
		return r, ErrRecord
	}
	d := json.NewDecoder(bytes.NewReader(data))
	tok, e := d.Token()
	if e != nil || tok != json.Delim('{') {
		return r, ErrRecord
	}
	seen := map[string]bool{}
	for d.More() {
		tok, e = d.Token()
		if e != nil {
			return r, ErrRecord
		}
		key, ok := tok.(string)
		if !ok || seen[key] {
			return r, ErrRecord
		}
		seen[key] = true
		var raw json.RawMessage
		if e = d.Decode(&raw); e != nil || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return r, ErrRecord
		}
		switch key {
		case "schema_version":
			e = json.Unmarshal(raw, &r.SchemaVersion)
		case "install_id":
			e = json.Unmarshal(raw, &r.InstallID)
		case "root":
			e = json.Unmarshal(raw, &r.Root)
		case "scope":
			e = json.Unmarshal(raw, &r.Scope)
		case "goos":
			e = json.Unmarshal(raw, &r.GOOS)
		case "agent":
			e = json.Unmarshal(raw, &r.Agent)
		case "client":
			e = json.Unmarshal(raw, &r.Client)
		default:
			return r, ErrRecord
		}
		if e != nil {
			return r, ErrRecord
		}
	}
	tok, e = d.Token()
	if e != nil || tok != json.Delim('}') || len(seen) != 7 {
		return r, ErrRecord
	}
	if _, e = d.Token(); e != io.EOF {
		return r, ErrRecord
	}
	return r, r.Validate(root)
}
