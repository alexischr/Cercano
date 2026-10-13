package enrollment

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"

	"cercano/source/server/internal/updatecoord/privdir"
)

func fixture(t *testing.T) string {
	t.Helper()
	parent, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	root := filepath.Join(parent, "installed")
	if _, e = privdir.Ensure(root); e != nil {
		t.Fatal(e)
	}
	a, c := names()
	for _, n := range []string{a, c} {
		if e = os.WriteFile(filepath.Join(root, n), []byte("inert launcher fixture"), 0700); e != nil {
			t.Fatal(e)
		}
	}
	return root
}
func created(t *testing.T, root string) Record {
	t.Helper()
	r, e := Create(context.Background(), root)
	if e != nil && !(runtime.GOOS == "windows" && errors.Is(e, ErrCommitted)) {
		t.Fatal(e)
	}
	got, e := Read(context.Background(), root)
	if e != nil || got != r {
		t.Fatal("published record unreadable", got, e)
	}
	return r
}
func TestEnrollmentExplicitStableAndNonOverwriting(t *testing.T) {
	root := fixture(t)
	path := filepath.Join(root, FileName)
	if _, e := Read(context.Background(), root); !os.IsNotExist(e) {
		t.Fatal("read enrolled or lost absence", e)
	}
	if _, e := os.Stat(path); !os.IsNotExist(e) {
		t.Fatal("read created marker")
	}
	first := created(t, root)
	before, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = Create(context.Background(), root); !errors.Is(e, ErrExists) {
		t.Fatal("existing identity overwritten", e)
	}
	after, e := os.ReadFile(path)
	if e != nil || string(before) != string(after) {
		t.Fatal("existing identity changed", e)
	}
	a, _ := names()
	if e = os.WriteFile(filepath.Join(root, a), []byte("updated inert launcher"), 0700); e != nil {
		t.Fatal(e)
	}
	next, e := Read(context.Background(), root)
	if e != nil || next.InstallID != first.InstallID {
		t.Fatal("identity depended on binary contents", e)
	}
	entries, e := os.ReadDir(root)
	if e != nil || len(entries) != 3 {
		t.Fatal("staging leftovers", entries, e)
	}
}
func TestEnrollmentConcurrentCreators(t *testing.T) {
	root := fixture(t)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var winner Record
	wins := 0
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, e := Create(context.Background(), root)
			if e == nil || errors.Is(e, ErrCommitted) {
				mu.Lock()
				wins++
				winner = r
				mu.Unlock()
			} else if !errors.Is(e, ErrExists) {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	got, e := Read(context.Background(), root)
	if wins != 1 || e != nil || got != winner {
		t.Fatal("not one stable identity", wins, got, e)
	}
}
func TestEnrollmentRejectsDevelopmentAndMissingLaunchers(t *testing.T) {
	root := fixture(t)
	if e := os.Mkdir(filepath.Join(filepath.Dir(root), ".git"), 0700); e != nil {
		t.Fatal(e)
	}
	if _, e := Create(context.Background(), root); !errors.Is(e, ErrUnsafe) {
		t.Fatal("development directory enrolled", e)
	}
	other := fixture(t)
	a, _ := names()
	if e := os.Remove(filepath.Join(other, a)); e != nil {
		t.Fatal(e)
	}
	if _, e := Create(context.Background(), other); !errors.Is(e, ErrUnsafe) {
		t.Fatal("incomplete installation enrolled", e)
	}
	for _, dir := range []string{root, other} {
		if _, e := os.Lstat(filepath.Join(dir, FileName)); !os.IsNotExist(e) {
			t.Fatal("refusal left marker", e)
		}
	}
}
func TestEnrollmentStrictFormatAndRootBinding(t *testing.T) {
	root := fixture(t)
	r := created(t, root)
	data, _ := json.Marshal(r)
	var fields map[string]json.RawMessage
	json.Unmarshal(data, &fields)
	for key := range fields {
		for _, null := range []bool{false, true} {
			var m map[string]json.RawMessage
			json.Unmarshal(data, &m)
			if null {
				m[key] = json.RawMessage("null")
			} else {
				delete(m, key)
			}
			b, _ := json.Marshal(m)
			if _, e := decode(b, root); e == nil {
				t.Fatal("missing/null accepted", key)
			}
		}
	}
	if _, e := decode(append(data, []byte(" {}")...), root); e == nil {
		t.Fatal("trailing document accepted")
	}
	r.SchemaVersion++
	b, _ := json.Marshal(r)
	if _, e := decode(b, root); e == nil {
		t.Fatal("future schema accepted")
	}
	if _, e := decode(data, filepath.Join(root, "other")); e == nil {
		t.Fatal("copied identity accepted")
	}
}
func TestEnrollmentCancellationAndSymlinkRefusal(t *testing.T) {
	root := fixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := Create(ctx, root); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	r := created(t, root)
	original := filepath.Join(root, FileName)
	moved := filepath.Join(root, "original")
	if e := os.Rename(original, moved); e != nil {
		t.Fatal(e)
	}
	if e := os.Symlink(moved, original); e != nil {
		t.Skipf("symlink fixture unavailable: %v", e)
	}
	if _, e := Read(context.Background(), root); !errors.Is(e, ErrUnsafe) {
		t.Fatal("linked marker accepted", e)
	}
	b, e := os.ReadFile(moved)
	if e != nil {
		t.Fatal(e)
	}
	got, e := decode(b, root)
	if e != nil || got != r {
		t.Fatal("refusal modified original", e)
	}
}

func TestEnrollmentPreservesCorruptExistingRecord(t *testing.T) {
	root := fixture(t)
	created(t, root)
	path := filepath.Join(root, FileName)
	if e := os.WriteFile(path, []byte("{bad identity"), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e := Read(context.Background(), root); e == nil {
		t.Fatal("corrupt record accepted")
	}
	if _, e := Create(context.Background(), root); !errors.Is(e, ErrExists) {
		t.Fatal("corrupt record replaced", e)
	}
	data, e := os.ReadFile(path)
	if e != nil || string(data) != "{bad identity" {
		t.Fatal("corrupt record changed", e)
	}
}
