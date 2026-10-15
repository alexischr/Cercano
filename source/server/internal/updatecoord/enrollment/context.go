package enrollment

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// ChildContext is a locator passed explicitly by a launcher, never authority.
// It carries no state-root override, executable command, or update consent.
type ChildContext struct{ Root, InstallID string }

// VerifyChildContext binds an explicit context to enrollment and to the actual
// child image. selectedPath and digest MUST come from independently verified
// installation state, not from the handoff, argv, environment, or PATH. The
// caller must use os.Executable for executable in production. This function
// grants neither activation nor package-manager ownership authority.
// maxBytes is a trusted finite image bound; no executable is run.
func VerifyChildContext(ctx context.Context, handoff ChildContext, executable, selectedPath, digest string, maxBytes int64) (Record, error) {
	fail := func() (Record, error) { return Record{}, ErrUnsafe }
	if ctx == nil || maxBytes <= 0 || maxBytes > 1<<30 {
		return fail()
	}
	if e := ctx.Err(); e != nil {
		return Record{}, e
	}
	if len(digest) != 64 || strings.ToLower(digest) != digest {
		return fail()
	}
	if _, e := hex.DecodeString(digest); e != nil {
		return fail()
	}
	record, e := Read(ctx, handoff.Root)
	if e != nil {
		return Record{}, e
	}
	if record.InstallID != handoff.InstallID {
		return fail()
	}
	for _, p := range []string{executable, selectedPath} {
		if !filepath.IsAbs(p) || filepath.Clean(p) != p {
			return fail()
		}
		canonical, e := filepath.EvalSymlinks(p)
		if e != nil || canonical != p {
			return fail()
		}
	}
	if executable != selectedPath {
		return fail()
	}
	rel, e := filepath.Rel(record.Root, selectedPath)
	if e != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fail()
	}
	a, c := names()
	if base := filepath.Base(executable); base != a && base != c {
		return fail()
	}
	named, e := os.Lstat(executable)
	if e != nil || !named.Mode().IsRegular() || named.Size() <= 0 || named.Size() > maxBytes {
		return fail()
	}
	f, e := os.Open(executable)
	if e != nil {
		return Record{}, e
	}
	defer f.Close()
	before, e := f.Stat()
	if e != nil || !os.SameFile(named, before) {
		return fail()
	}
	hash := sha256.New()
	reader := io.LimitReader(f, maxBytes+1)
	buf := make([]byte, 64*1024)
	var total int64
	for {
		if e := ctx.Err(); e != nil {
			return Record{}, e
		}
		n, e := reader.Read(buf)
		total += int64(n)
		if n > 0 {
			_, _ = hash.Write(buf[:n])
		}
		if total > maxBytes {
			return fail()
		}
		if e == io.EOF {
			break
		}
		if e != nil {
			return Record{}, e
		}
	}
	after, e := f.Stat()
	if e != nil || after.Size() != before.Size() || !after.ModTime().Equal(before.ModTime()) || after.Mode() != before.Mode() {
		return fail()
	}
	current, e := os.Lstat(executable)
	if e != nil || !current.Mode().IsRegular() || !os.SameFile(before, current) || total != before.Size() || hex.EncodeToString(hash.Sum(nil)) != digest {
		return fail()
	}
	final, e := Read(ctx, handoff.Root)
	if e != nil {
		return Record{}, e
	}
	if final != record {
		return fail()
	}
	return record, nil
}
