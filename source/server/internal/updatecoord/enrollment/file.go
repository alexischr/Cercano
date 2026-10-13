package enrollment

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"

	"cercano/source/server/internal/updatecoord/privdir"
)

// checkRoot refuses implicit directory creation/repair and obvious development
// trees. The parent chain is caller-trusted; package ownership still must be
// corroborated independently before the caller requests enrollment.
func checkRoot(root string) error {
	if e := privdir.VerifyExisting(root); e != nil {
		return errors.Join(ErrUnsafe, e)
	}
	canonical, e := filepath.EvalSymlinks(root)
	if e != nil || canonical != root {
		return ErrUnsafe
	}
	for p := root; ; p = filepath.Dir(p) {
		if _, e = os.Lstat(filepath.Join(p, ".git")); e == nil || !os.IsNotExist(e) {
			return ErrUnsafe
		}
		next := filepath.Dir(p)
		if p == next {
			break
		}
	}
	a, c := names()
	for _, name := range []string{a, c} {
		info, e := os.Lstat(filepath.Join(root, name))
		if e != nil || !info.Mode().IsRegular() {
			return ErrUnsafe
		}
	}
	return nil
}

// Read never enrolls or migrates. A copied marker with a different root binding,
// links, nonprivate file access or unknown formats refuse unchanged.
func Read(ctx context.Context, root string) (Record, error) {
	var zero Record
	if ctx == nil {
		return zero, ErrRecord
	}
	if e := ctx.Err(); e != nil {
		return zero, e
	}
	if e := checkRoot(root); e != nil {
		return zero, e
	}
	parent, e := os.Open(root)
	if e != nil {
		return zero, e
	}
	defer parent.Close()
	bound, e := parent.Stat()
	if e != nil {
		return zero, e
	}
	path := filepath.Join(root, FileName)
	named, e := os.Lstat(path)
	if e != nil {
		return zero, e
	}
	if !named.Mode().IsRegular() {
		return zero, ErrUnsafe
	}
	f, e := privdir.OpenPrivateFile(path)
	if e != nil {
		return zero, errors.Join(ErrUnsafe, e)
	}
	defer f.Close()
	info, e := f.Stat()
	if e != nil || !os.SameFile(named, info) || info.Size() > MaxRecordBytes {
		return zero, ErrUnsafe
	}
	data, e := io.ReadAll(io.LimitReader(f, MaxRecordBytes+1))
	if e != nil {
		return zero, e
	}
	record, e := decode(data, root)
	if e != nil {
		return zero, e
	}
	after, e := f.Stat()
	if e != nil || after.Size() != info.Size() || !after.ModTime().Equal(info.ModTime()) {
		return zero, ErrUnsafe
	}
	current, e := os.Lstat(path)
	if e != nil || !current.Mode().IsRegular() || !os.SameFile(info, current) {
		return zero, ErrUnsafe
	}
	currentRoot, e := os.Lstat(root)
	if e != nil || !currentRoot.IsDir() || !os.SameFile(bound, currentRoot) {
		return zero, ErrUnsafe
	}
	if e = ctx.Err(); e != nil {
		return zero, e
	}
	return record, nil
}

// Create is installer-only, explicit per-user enrollment. It never adopts on
// startup and never overwrites a record. A returned ErrCommitted means publication
// happened and callers must inspect it rather than reset or retry replacement.
// No binaries are written, executed, or assumed to be signed by this operation.
func Create(ctx context.Context, root string) (record Record, err error) {
	if ctx == nil {
		return record, ErrRecord
	}
	if err = ctx.Err(); err != nil {
		return record, err
	}
	if err = checkRoot(root); err != nil {
		return record, err
	}
	parent, e := os.Open(root)
	if e != nil {
		return record, e
	}
	defer parent.Close()
	bound, e := parent.Stat()
	if e != nil {
		return record, e
	}
	final := filepath.Join(root, FileName)
	if _, e = os.Lstat(final); e == nil {
		return record, ErrExists
	} else if !os.IsNotExist(e) {
		return record, e
	}
	record, err = fresh(root)
	if err != nil {
		return record, err
	}
	data, err := json.Marshal(record)
	if err != nil || len(data) > MaxRecordBytes {
		return Record{}, ErrRecord
	}
	temp := filepath.Join(root, ".identity-"+record.InstallID+".tmp")
	f, err := privdir.CreatePrivateFile(temp)
	if err != nil {
		return Record{}, err
	}
	defer f.Close()
	identity, e := f.Stat()
	if e != nil {
		return Record{}, e
	}
	// The handle remains pinned through publication/cleanup. Unknown replacements
	// are retained and reported; cleanup never traverses or removes directories.
	defer func() {
		current, e := os.Lstat(temp)
		if os.IsNotExist(e) {
			return
		}
		if e != nil || !current.Mode().IsRegular() || !os.SameFile(identity, current) {
			err = errors.Join(err, ErrUnsafe)
			return
		}
		if e = os.Remove(temp); e != nil {
			err = errors.Join(err, e)
		}
	}()
	if _, err = f.Write(data); err != nil {
		return Record{}, err
	}
	if err = f.Sync(); err != nil {
		return Record{}, err
	}
	if err = ctx.Err(); err != nil {
		return Record{}, err
	}
	current, e := os.Lstat(root)
	if e != nil || !current.IsDir() || !os.SameFile(bound, current) {
		return Record{}, ErrUnsafe
	}
	entry, e := os.Lstat(temp)
	if e != nil || !entry.Mode().IsRegular() || !os.SameFile(identity, entry) {
		return Record{}, ErrUnsafe
	}
	// Same-directory hard-link publication is atomic and refuses an existing
	// destination. It never replaces somebody else's identity record.
	if err = os.Link(temp, final); err != nil {
		if os.IsExist(err) {
			return Record{}, ErrExists
		}
		return Record{}, err
	}
	if err = os.Remove(temp); err != nil {
		return record, errors.Join(ErrCommitted, err)
	}
	if err = f.Close(); err != nil {
		return record, errors.Join(ErrCommitted, err)
	}
	if err = syncIdentityDirectory(parent); err != nil {
		return record, errors.Join(ErrCommitted, err)
	}
	observed, e := Read(ctx, root)
	if e != nil || observed != record {
		return record, errors.Join(ErrCommitted, e, ErrUnsafe)
	}
	return record, nil
}
