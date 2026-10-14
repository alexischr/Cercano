package builtins

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cercano/source/server/internal/capabilities"
)

// grepFallbackFixture writes a small tree whose contents exercise the
// portable-regex dialects that differ between POSIX basic (grep's default,
// the current fallback) and POSIX extended (the documented contract):
// alternation, grouping, and repetition.
func grepFallbackFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"notes.txt":  "alpha blooms\nbeta rays\nplain text\n",
		"group.txt":  "saw foobarbar here\nno match here\n",
		"escape.txt": "version v1.2 shipped\nversion v1x2 not\n",
		"README.md":  "alpha also in markdown\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// rowContent returns the concatenated content of all rows, for match-shape
// assertions without depending on row ordering across files.
func rowContent(res *capabilities.Result) string {
	var b strings.Builder
	for _, r := range res.Rows {
		if s, ok := r["content"].(string); ok {
			b.WriteString(s)
			b.WriteString("\n")
		}
	}
	return b.String()
}

// The grep fallback must honor extended-regex alternation ("foo|bar"). With
// POSIX basic regex (the fallback's current dialect), "|" is a literal, so the
// pattern matches nothing.
func TestGrepFallback_Alternation(t *testing.T) {
	dir := grepFallbackFixture(t)
	res, err := grepRunGrep(t.Context(), grepArgs{Pattern: "alpha|beta", Path: dir})
	if err != nil {
		t.Fatalf("grep fallback: %v", err)
	}
	content := rowContent(res)
	if !strings.Contains(content, "alpha") || !strings.Contains(content, "beta") {
		t.Errorf("alternation 'alpha|beta' should match both branches, got rows: %v (content=%q)", res.Rows, content)
	}
}

// Grouping with "(...)" and one-or-more "+" is extended regex. POSIX basic
// treats all three as literals, so this matches nothing without -E.
func TestGrepFallback_GroupingAndRepetition(t *testing.T) {
	dir := grepFallbackFixture(t)
	res, err := grepRunGrep(t.Context(), grepArgs{Pattern: "foo(bar)+", Path: dir})
	if err != nil {
		t.Fatalf("grep fallback: %v", err)
	}
	if len(res.Rows) == 0 {
		t.Errorf("pattern 'foo(bar)+' should match 'foobarbar' via extended regex; basic regex treats ( ) + as literals")
	}
	for _, r := range res.Rows {
		if s, _ := r["content"].(string); !strings.Contains(s, "foobarbar") {
			t.Errorf("unexpected match %q", s)
		}
	}
}

// Escaped literals stay literal in both dialects — a portability guard: the
// documented contract is "portable common regex", which includes escaped
// literal metacharacters.
func TestGrepFallback_EscapedLiteralDot(t *testing.T) {
	dir := grepFallbackFixture(t)
	res, err := grepRunGrep(t.Context(), grepArgs{Pattern: `v1\.2`, Path: dir})
	if err != nil {
		t.Fatalf("grep fallback: %v", err)
	}
	content := rowContent(res)
	if !strings.Contains(content, "v1.2") {
		t.Errorf(`pattern 'v1\.2' should match the literal 'v1.2'`)
	}
	if strings.Contains(content, "v1x2") {
		t.Errorf(`pattern 'v1\.2' must not match 'v1x2' — the dot must stay escaped-literal`)
	}
}

// Zero matches are a normal result, not an error: exit code 1 maps to empty
// rows with a "0 matches" detail.
func TestGrepFallback_NoMatch(t *testing.T) {
	dir := grepFallbackFixture(t)
	res, err := grepRunGrep(t.Context(), grepArgs{Pattern: "zebra", Path: dir})
	if err != nil {
		t.Fatalf("grep fallback: %v", err)
	}
	if len(res.Rows) != 0 {
		t.Errorf("expected zero rows, got %d", len(res.Rows))
	}
	if res.Detail != "0 matches" {
		t.Errorf("detail = %q, want %q", res.Detail, "0 matches")
	}
}

// A malformed pattern (unbalanced group) must surface as an error, not as
// "zero matches". Without -E, grep's basic dialect treats "(" as a literal and
// silently returns no matches — hiding the authoring mistake.
func TestGrepFallback_MalformedPattern_Errors(t *testing.T) {
	dir := grepFallbackFixture(t)
	_, err := grepRunGrep(t.Context(), grepArgs{Pattern: "foo(", Path: dir})
	if err == nil {
		t.Fatal("malformed pattern 'foo(' must return an error (unbalanced group), not a silent zero-match result")
	}
}

// The fallback must keep the existing path scoping: only files under Path are
// searched. Fixture file lives outside the subdirectory passed as Path.
func TestGrepFallback_PathScopesSearch(t *testing.T) {
	dir := grepFallbackFixture(t)
	sub := filepath.Join(dir, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "inner.txt"), []byte("alpha inside\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := grepRunGrep(t.Context(), grepArgs{Pattern: "alpha", Path: sub})
	if err != nil {
		t.Fatalf("grep fallback: %v", err)
	}
	if len(res.Rows) != 1 {
		t.Fatalf("expected exactly one row from the scoped path, got %d", len(res.Rows))
	}
	if p, _ := res.Rows[0]["path"].(string); !strings.Contains(p, "inner.txt") {
		t.Errorf("row path = %q, want the inner.txt file", p)
	}
}

// Case-insensitive matching keeps working in the fallback.
func TestGrepFallback_CaseInsensitive(t *testing.T) {
	dir := grepFallbackFixture(t)
	res, err := grepRunGrep(t.Context(), grepArgs{Pattern: "ALPHA", Path: dir, CaseInsensitive: true})
	if err != nil {
		t.Fatalf("grep fallback: %v", err)
	}
	if len(res.Rows) == 0 {
		t.Error("case-insensitive 'ALPHA' should match 'alpha'")
	}
}

// Glob filtering (--include) keeps working in the fallback.
func TestGrepFallback_GlobFilter(t *testing.T) {
	dir := grepFallbackFixture(t)
	res, err := grepRunGrep(t.Context(), grepArgs{Pattern: "alpha", Path: dir, Glob: "*.txt"})
	if err != nil {
		t.Fatalf("grep fallback: %v", err)
	}
	for _, r := range res.Rows {
		if p, _ := r["path"].(string); strings.Contains(p, "README.md") {
			t.Error("glob '*.txt' must exclude README.md")
		}
	}
	if len(res.Rows) == 0 {
		t.Error("expected matches from .txt files")
	}
}
