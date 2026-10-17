package main

// Tests for the early install-context wiring (child_context.go): the
// reserved private locator flags are claimed before any normal flag parsing,
// config load, or agent launch. The running test binary is never the
// enrolled image, so a present locator context must always refuse here.

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
		{"cercano-cli", "-r"},
		{"cercano-cli", "--mdtest"},
		{"cercano-cli"},
		{"cercano-cli", "--", installcontext.FlagInstallRoot, "/opt/x", installcontext.FlagInstallID, "abc"},
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
		"equals root":       {"cercano-cli", installcontext.FlagInstallRoot + "=" + root, installcontext.FlagInstallID, "abc"},
		"equals id":         {"cercano-cli", installcontext.FlagInstallRoot, root, installcontext.FlagInstallID + "=abc"},
		"duplicate id":      {"cercano-cli", installcontext.FlagInstallRoot, root, installcontext.FlagInstallID, "abc", installcontext.FlagInstallID, "abc"},
		"partial root only": {"cercano-cli", installcontext.FlagInstallRoot, root},
		"partial id only":   {"cercano-cli", installcontext.FlagInstallID, "abc"},
		"missing value":     {"cercano-cli", installcontext.FlagInstallID},
		"invalid id":        {"cercano-cli", installcontext.FlagInstallRoot, root, installcontext.FlagInstallID, "../escape"},
		"relative root":     {"cercano-cli", installcontext.FlagInstallRoot, "opt/cercano", installcontext.FlagInstallID, "abc"},
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
	root := t.TempDir()
	os.Args = []string{"cercano-cli", "-r", installcontext.FlagInstallRoot, root, installcontext.FlagInstallID, strings.Repeat("a", 32)}
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
	want := []string{"cercano-cli", "-r"}
	if !reflect.DeepEqual(os.Args, want) {
		t.Fatalf("os.Args = %v, want reserved flags removed %v", os.Args, want)
	}
}
