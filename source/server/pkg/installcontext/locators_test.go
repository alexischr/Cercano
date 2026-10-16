package installcontext_test

import (
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"cercano/source/server/pkg/installcontext"
)

func canonicalRoot(t *testing.T) string {
	t.Helper()
	abs, err := filepath.Abs(filepath.Join("opt", "cercano"))
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

const validInstallID = "0123456789abcdef0123456789abcdef"

func TestParseRejectsMalformedReservedVariants(t *testing.T) {
	root := canonicalRoot(t)
	for name, args := range map[string][]string{
		"equals root":      {installcontext.FlagInstallRoot + "=" + root, installcontext.FlagInstallID, validInstallID},
		"equals id":        {installcontext.FlagInstallRoot, root, installcontext.FlagInstallID + "=" + validInstallID},
		"equals both":      {installcontext.FlagInstallRoot + "=" + root, installcontext.FlagInstallID + "=" + validInstallID},
		"equals empty root": {installcontext.FlagInstallRoot + "=", installcontext.FlagInstallID, validInstallID},
		"duplicate root":   {installcontext.FlagInstallRoot, root, installcontext.FlagInstallRoot, root, installcontext.FlagInstallID, validInstallID},
		"duplicate id":     {installcontext.FlagInstallRoot, root, installcontext.FlagInstallID, validInstallID, installcontext.FlagInstallID, validInstallID},
		"partial root only": {installcontext.FlagInstallRoot, root},
		"partial id only":   {installcontext.FlagInstallID, validInstallID},
		"missing root value": {installcontext.FlagInstallRoot},
		"missing id value":  {installcontext.FlagInstallID},
		"root value is id flag": {installcontext.FlagInstallRoot, installcontext.FlagInstallID, validInstallID},
		"root value flag-like": {installcontext.FlagInstallRoot, "-x", installcontext.FlagInstallID, validInstallID},
		"empty root value":  {installcontext.FlagInstallRoot, "", installcontext.FlagInstallID, validInstallID},
		"relative root":     {installcontext.FlagInstallRoot, "opt/cercano", installcontext.FlagInstallID, validInstallID},
		"unclean root":      {installcontext.FlagInstallRoot, root + string(filepath.Separator) + "x" + string(filepath.Separator) + "..", installcontext.FlagInstallID, validInstallID},
		"trailing slash root": {installcontext.FlagInstallRoot, root + string(filepath.Separator), installcontext.FlagInstallID, validInstallID},
		"uppercase install id": {installcontext.FlagInstallRoot, root, installcontext.FlagInstallID, "ABC"},
		"path install id":   {installcontext.FlagInstallRoot, root, installcontext.FlagInstallID, "../escape"},
		"dos device install id": {installcontext.FlagInstallRoot, root, installcontext.FlagInstallID, "con"},
		"dot install id":    {installcontext.FlagInstallRoot, root, installcontext.FlagInstallID, "."},
		"empty install id":  {installcontext.FlagInstallRoot, root, installcontext.FlagInstallID, ""},
	} {
		t.Run(name, func(t *testing.T) {
			locators, found, remaining, err := installcontext.Parse(args)
			if err == nil {
				t.Fatalf("args %v: accepted (locators %+v, found %t, remaining %v)", args, locators, found, remaining)
			}
			if !errors.Is(err, installcontext.ErrMalformed) && !errors.Is(err, installcontext.ErrNoLocators) {
				t.Fatalf("args %v: error %v is not a locator-shape error", args, err)
			}
			if found || remaining != nil {
				t.Fatalf("args %v: rejection returned found=%t remaining=%v, want false/nil", args, found, remaining)
			}
		})
	}
}

func TestParseRemovesLocatorsAndPreservesNormalArgv(t *testing.T) {
	root := canonicalRoot(t)
	idFirst := []string{installcontext.FlagInstallID, validInstallID, installcontext.FlagInstallRoot, root}
	locators, found, remaining, err := installcontext.Parse(idFirst)
	if err != nil || !found {
		t.Fatalf("reordered pair rejected: found=%t err=%v", found, err)
	}
	if locators.InstallRoot != root || locators.InstallID != validInstallID || !locators.Present() {
		t.Fatalf("locators = %+v, want root %q id %q", locators, root, validInstallID)
	}
	if len(remaining) != 0 {
		t.Fatalf("remaining = %v, want empty", remaining)
	}

	mixed := []string{"--mcp", installcontext.FlagInstallRoot, root, "agent", installcontext.FlagInstallID, validInstallID}
	locators, found, remaining, err = installcontext.Parse(mixed)
	if err != nil || !found {
		t.Fatalf("mixed argv rejected: found=%t err=%v", found, err)
	}
	if !reflect.DeepEqual(remaining, []string{"--mcp", "agent"}) {
		t.Fatalf("remaining = %v, want [--mcp agent] (order preserved)", remaining)
	}
	if locators.InstallRoot != root || locators.InstallID != validInstallID {
		t.Fatalf("locators = %+v", locators)
	}

	// No context: argv untouched, startup unchanged.
	plain := []string{"agent", "--mcp", "-r"}
	locators, found, remaining, err = installcontext.Parse(plain)
	if err != nil || found || locators.Present() {
		t.Fatalf("plain argv claimed: found=%t err=%v", found, err)
	}
	if !reflect.DeepEqual(remaining, plain) {
		t.Fatalf("remaining = %v, want argv unchanged %v", remaining, plain)
	}

	// After the terminator, reserved flags are ordinary positional tokens.
	terminated := []string{"--", installcontext.FlagInstallRoot, root, installcontext.FlagInstallID, validInstallID}
	locators, found, remaining, err = installcontext.Parse(terminated)
	if err != nil || found || locators.Present() {
		t.Fatalf("post-terminator tokens claimed as flags: found=%t err=%v", found, err)
	}
	if !reflect.DeepEqual(remaining, terminated) {
		t.Fatalf("remaining = %v, want argv with terminator preserved %v", remaining, terminated)
	}
}

func TestLocatorArgsEmitsOnlyLocators(t *testing.T) {
	root := canonicalRoot(t)
	args, err := installcontext.LocatorArgs(root, validInstallID)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{installcontext.FlagInstallRoot, root, installcontext.FlagInstallID, validInstallID}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("locator args = %v, want %v (locators only; never a digest or executable path)", args, want)
	}
	if _, err = installcontext.LocatorArgs("opt/cercano", validInstallID); err == nil {
		t.Fatal("relative root accepted")
	}
	if _, err = installcontext.LocatorArgs(root, "bad/id"); err == nil {
		t.Fatal("invalid install id accepted")
	}

	// Round trip: a launcher-built argv parses back into the same locators.
	locators, found, remaining, err := installcontext.Parse(args)
	if err != nil || !found || len(remaining) != 0 {
		t.Fatalf("round trip failed: found=%t remaining=%v err=%v", found, remaining, err)
	}
	if locators.InstallRoot != root || locators.InstallID != validInstallID {
		t.Fatalf("round trip locators = %+v", locators)
	}
}

func TestLocatorArgsNeverCarriesAuthority(t *testing.T) {
	root := canonicalRoot(t)
	args, err := installcontext.LocatorArgs(root, validInstallID)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(args, " ")
	for _, forbidden := range []string{"sha256", "digest", ".exe", "/versions/"} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("locator args %q carry authority-like token %q", joined, forbidden)
		}
	}
}
