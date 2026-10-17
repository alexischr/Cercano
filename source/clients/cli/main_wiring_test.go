package main

// Wiring test for the real main(): the install-context claim must run at the
// TOP of main(), before flag parsing, so a child launched with the locators
// proves its executable and client role before anything else, and
// --version/tui-handoff-protocol behavior stays reachable only AFTER the
// gate. Exact-match guard against regression.

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// mainEncodedLines holds the real main.go source lines, read at test init so
// the assertions below are made against the actual shipped wiring.
var mainEncodedLines = loadMainLines()

func loadMainLines() []string {
	data, err := os.ReadFile("main.go")
	if err != nil {
		panic(err)
	}
	return strings.Split(string(data), "\n")
}

func TestMainWiresInstallContextClaimBeforeFlagParsing(t *testing.T) {
	re := regexp.MustCompile(`claimInstallContext\(os\.Args\); !ok`)
	var got []string
	for _, line := range mainEncodedLines {
		if re.MatchString(line) {
			got = append(got, strings.TrimSpace(line))
		}
	}
	if len(got) != 1 || got[0] != "if v, code, ok := claimInstallContext(os.Args); !ok {" {
		t.Fatalf("claimInstallContext wiring: got %q, want exactly [if v, code, ok := claimInstallContext(os.Args); !ok {]", got)
	}
	claimAt, flagAt, versionAt := -1, -1, -1
	for i, line := range mainEncodedLines {
		switch {
		case strings.Contains(line, "claimInstallContext(os.Args)"):
			if claimAt == -1 {
				claimAt = i
			}
		case strings.Contains(line, "flag.Parse()"):
			if flagAt == -1 {
				flagAt = i
			}
		case strings.Contains(line, `*showVersion {`):
			if versionAt == -1 {
				versionAt = i
			}
		}
	}
	if claimAt < 0 || flagAt < 0 || versionAt < 0 {
		t.Fatalf("main wiring incomplete: claimAt=%d flagAt=%d versionAt=%d", claimAt, flagAt, versionAt)
	}
	// The claim must precede flag.Parse() (the reserved flags are unknown to
	// flag parsing) and therefore also the --version paths.
	if !(claimAt < flagAt && claimAt < versionAt) {
		t.Fatalf("order wrong: claim(%d) flag.Parse(%d) version(%d) — the claim must run first so versioned startup cannot bypass required verification", claimAt, flagAt, versionAt)
	}
	// Refusal must exit with the returned code.
	exitRe := regexp.MustCompile(`os\.Exit\(code\)`)
	found := false
	for _, line := range mainEncodedLines {
		if exitRe.MatchString(line) {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("main must os.Exit(code) when claimInstallContext refuses startup")
	}
}

func TestMainWiresVerifiedContextPropagation(t *testing.T) {
	// The verified result of the early gate must be hoisted (not scoped to
	// the if-statement) and threaded explicitly through runCLI to the UI
	// model — never parked in a process-global. No context means the
	// verified value is the absent zero value and downstream consumers
	// keep their unchanged behavior.
	assert := func(what, pattern string, want []string) {
		t.Helper()
		re := regexp.MustCompile(pattern)
		var got []string
		for _, line := range mainEncodedLines {
			if re.MatchString(line) {
				got = append(got, strings.TrimSpace(line))
			}
		}
		if len(got) != len(want) {
			t.Fatalf("%s: pattern %q matched %d lines, want %d (got %q)", what, pattern, len(got), len(want), got)
		}
		for i, wantLine := range want {
			if got[i] != wantLine {
				t.Fatalf("%s: line %d = %q, want %q", what, i, got[i], wantLine)
			}
		}
	}
	assert("verified declaration", `var verified installcontext\.Verified`,
		[]string{"var verified installcontext.Verified"})
	assert("verified hoist", `verified = v$`,
		[]string{"verified = v"})
	assert("runCLI verified", `runCLI\(cfg, openHistory, openWizard, seedDoc, verified\)`,
		[]string{"runCLI(cfg, openHistory, openWizard, seedDoc, verified)"})
	assert("model verified", `WithTUIVersion\(version\)\.WithVerifiedContext\(verified\)`,
		[]string{"m := cliui.New(ag, openHistoryOnStart).WithTUIVersion(version).WithVerifiedContext(verified)"})
}
