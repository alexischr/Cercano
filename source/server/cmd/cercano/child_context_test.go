package main

// Tests for the early install-context wiring (child_context.go): the
// reserved private locator flags are claimed before any subcommand dispatch
// or normal initialization. In-process tests cover claimInstallContext's
// decisions; the subprocess fixtures (runMainFixture) prove the REAL main()
// refuses malformed and unverified locator invocations before creating any
// files or doing any normal startup.

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"cercano/source/server/pkg/installcontext"
)

func restoreArgs(t *testing.T) {
	t.Helper()
	argv := os.Args
	t.Cleanup(func() { os.Args = argv })
}

func TestClaimInstallContextNoLocatorsLeavesStartupUnchanged(t *testing.T) {
	restoreArgs(t)
	for _, args := range [][]string{
		{"cercano", "agent", "--mcp"},
		{"cercano", "version"},
		{"cercano"},
		{"cercano", "--", installcontext.FlagInstallRoot, "/opt/x", installcontext.FlagInstallID, "abc"},
	} {
		os.Args = args
		verified, code, ok := claimInstallContext(os.Args)
		if !ok || code != 0 {
			t.Fatalf("args %v: refused with code %d, want continue", args, code)
		}
		if verified.Present() {
			t.Fatalf("args %v: verified context minted without locators", args)
		}
		if !reflect.DeepEqual(os.Args, args) {
			t.Fatalf("args %v: os.Args rewritten to %v without locators", args, os.Args)
		}
	}
}

func TestClaimInstallContextRejectsMalformedReservedVariants(t *testing.T) {
	restoreArgs(t)
	root, err := filepath.Abs(filepath.Join("opt", "cercano"))
	if err != nil {
		t.Fatal(err)
	}
	for name, args := range map[string][]string{
		"equals root":       {"cercano", installcontext.FlagInstallRoot + "=" + root, installcontext.FlagInstallID, "abc"},
		"equals id":         {"cercano", installcontext.FlagInstallRoot, root, installcontext.FlagInstallID + "=abc"},
		"duplicate root":    {"cercano", installcontext.FlagInstallRoot, root, installcontext.FlagInstallRoot, root, installcontext.FlagInstallID, "abc"},
		"partial root only": {"cercano", installcontext.FlagInstallRoot, root},
		"partial id only":   {"cercano", installcontext.FlagInstallID, "abc"},
		"missing value":     {"cercano", installcontext.FlagInstallID},
		"invalid id":        {"cercano", installcontext.FlagInstallRoot, root, installcontext.FlagInstallID, "../escape"},
	} {
		t.Run(name, func(t *testing.T) {
			os.Args = args
			_, code, ok := claimInstallContext(os.Args)
			if ok {
				t.Fatalf("args %v: malformed locators accepted", args)
			}
			if code != locatorUsageExit {
				t.Fatalf("args %v: code = %d, want %d", args, code, locatorUsageExit)
			}
		})
	}
}

func TestClaimInstallContextValidLocatorsStripAndRefuseUnverifiedChild(t *testing.T) {
	restoreArgs(t)
	// A syntactically valid locator pair pointing at a directory with no
	// enrollment: verification must refuse before any normal effect. The
	// running test binary is not the enrolled image.
	root := t.TempDir()
	os.Args = []string{"cercano", "--mcp", installcontext.FlagInstallRoot, root, installcontext.FlagInstallID, strings.Repeat("a", 32)}
	verified, code, ok := claimInstallContext(os.Args)
	if ok {
		t.Fatal("unverified child context accepted")
	}
	if code != childRefusalExit {
		t.Fatalf("code = %d, want %d (verification refusal)", code, childRefusalExit)
	}
	if verified.Present() {
		t.Fatal("verified context returned alongside verification refusal")
	}
	want := []string{"cercano", "--mcp"}
	if !reflect.DeepEqual(os.Args, want) {
		t.Fatalf("os.Args = %v, want reserved flags removed %v", os.Args, want)
	}
}

func TestMainSubprocessMalformedInstallLocatorsRejected(t *testing.T) {
	_, stderr, code := runMainFixture(t,
		installcontext.FlagInstallRoot+"=/opt/cercano",
		installcontext.FlagInstallID, strings.Repeat("a", 32))
	if code != locatorUsageExit {
		t.Fatalf("exit code = %d, want %d; stderr: %s", code, locatorUsageExit, stderr)
	}
	if !strings.Contains(stderr, "malformed") && !strings.Contains(stderr, "separate-value") {
		t.Fatalf("stderr = %q, want malformed-locator diagnostic", stderr)
	}
}

func TestMainSubprocessPartialInstallLocatorsRejected(t *testing.T) {
	_, stderr, code := runMainFixture(t, installcontext.FlagInstallID, strings.Repeat("a", 32))
	if code != locatorUsageExit {
		t.Fatalf("exit code = %d, want %d; stderr: %s", code, locatorUsageExit, stderr)
	}
}

func TestMainSubprocessUnverifiedInstallContextRefusesStartup(t *testing.T) {
	// A valid locator pair for a root with no enrollment: the real main()
	// must refuse startup (exit 1) before any config, DB, cache, or state
	// initialization — runMainFixture fails the test if anything is created.
	root := t.TempDir()
	_, stderr, code := runMainFixture(t,
		installcontext.FlagInstallRoot, root,
		installcontext.FlagInstallID, strings.Repeat("a", 32))
	if code != childRefusalExit {
		t.Fatalf("exit code = %d, want %d; stderr: %s", code, childRefusalExit, stderr)
	}
	if !strings.Contains(stderr, "refusing startup") {
		t.Fatalf("stderr = %q, want refusal diagnostic", stderr)
	}
}
