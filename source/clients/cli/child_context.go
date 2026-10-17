package main

// Early wiring of the approved private argv installation locators for
// cercano-cli. The reserved flags --internal-install-root and
// --internal-install-id are parsed and removed from argv BEFORE the normal
// flag set (they are unknown to flag.Parse) and before any config load or
// agent launch. A child launched with the locators must prove — via the
// shared installcontext package — that its ACTUAL executable and the fixed
// launcher role (client) match the protected completed selection for that
// installation, or startup is refused before any initialization effect.
// Without locators, startup is unchanged.

import (
	"context"
	"fmt"
	"os"

	"cercano/source/server/pkg/installcontext"
)

const (
	childRefusalExit = 1
	locatorUsageExit = 2
)

// claimInstallContext consumes the full os.Args. It parses and removes the
// reserved installation-locator flags, rewrites os.Args to the remaining
// argv, and when locators are present runs the early child verification.
// It returns (code, false) when the caller must exit immediately; (0, true)
// means startup may continue (with os.Args stripped of any locators) and
// carries the verified installation context, absent without locators.
func claimInstallContext(argv []string) (installcontext.Verified, int, bool) {
	locators, found, remaining, err := installcontext.Parse(argv[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "cercano-cli:", err)
		return installcontext.Verified{}, locatorUsageExit, false
	}
	if !found {
		return installcontext.Verified{}, 0, true
	}
	os.Args = append(os.Args[:1:1], remaining...)
	verified, err := installcontext.VerifyStartupVerified(context.Background(), locators, true)
	if err != nil {
		fmt.Fprintln(os.Stderr, "cercano-cli: refusing startup: versioned child failed installation verification")
		return installcontext.Verified{}, childRefusalExit, false
	}
	return verified, 0, true
}
