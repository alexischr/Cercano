// Package installcontext implements the approved private argv installation
// locators and the versioned-child startup verification shared by the
// cercano agent and cercano-cli mains (efforts/cross-platform-updates
// installation-context approval).
//
// A trusted fixed launcher may tell a versioned child which installation it
// belongs to by passing the two reserved flags --internal-install-root and
// --internal-install-id. The values are LOCATORS, never authority: the child
// parses and removes them before any normal initialization, then
// independently verifies its actual executable and launcher role against the
// protected completed selection for that installation. The build helper
// emits only the two locator flags — never a digest or an executable path —
// and a process started without locators behaves exactly as before.
package installcontext

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"cercano/source/server/internal/updatecoord/state"
)

// Reserved private argv flags carrying the installation locators. They are
// parsed and removed by the mains before the normal flag set; unknown to the
// normal flag package, so they must never survive into flag.Parse.
const (
	FlagInstallRoot = "--internal-install-root"
	FlagInstallID   = "--internal-install-id"
)

// flagTerminator is the standard end-of-flags marker. Locators after it are
// ordinary positional arguments and are never claimed here.
const flagTerminator = "--"

var (
	// ErrMalformed reports a rejected reserved-locator argv shape: equals
	// forms, duplicates, partial pairs, missing values, flag-like values,
	// or values that are not canonical locators.
	ErrMalformed = errors.New("installcontext: malformed reserved installation locator arguments")
	// ErrNoLocators reports an operation requiring locators that are absent.
	ErrNoLocators = errors.New("installcontext: no installation locators")
)

// Locators are the parsed installation locator values. They identify WHICH
// installation a child belongs to; they carry no authority of their own.
type Locators struct {
	InstallRoot string
	InstallID   string
}

// Present reports whether both locator values are set.
func (l Locators) Present() bool { return l.InstallRoot != "" && l.InstallID != "" }

// Validate enforces the canonical locator spellings. It is a fail-fast
// shape check only: the enrollment record and protected state remain the
// sole authority during verification.
func (l Locators) Validate() error {
	if !l.Present() {
		return fmt.Errorf("%w: both --internal-install-root and --internal-install-id are required", ErrNoLocators)
	}
	if err := validateRoot(l.InstallRoot); err != nil {
		return err
	}
	if err := state.ValidateInstallID(l.InstallID); err != nil {
		return fmt.Errorf("%w: invalid install id: %v", ErrMalformed, err)
	}
	return nil
}

// validateRoot requires a canonical absolute path with no NUL bytes.
func validateRoot(root string) error {
	if root == "" || !filepath.IsAbs(root) || filepath.Clean(root) != root || strings.ContainsRune(root, 0) {
		return fmt.Errorf("%w: install root must be a canonical absolute path", ErrMalformed)
	}
	return nil
}

// Parse extracts the approved reserved private locator flags from args and
// returns the locators, whether any were present, and the remaining argv
// (with the reserved flags removed, everything else — including the "--"
// terminator and everything after it — preserved in order).
//
// Rejected shapes fail before any value is accepted: the "--flag=value"
// equals form, duplicated flags, partial pairs (one flag without the
// other), a missing trailing value, a flag-like value, and values that are
// not canonical locators. Reserved flags after a bare "--" are ordinary
// positional arguments and are never claimed.
//
// When found is false the remaining argv equals args unchanged (minus
// nothing); a caller with no context continues startup untouched.
func Parse(args []string) (locators Locators, found bool, remaining []string, err error) {
	var root, id string
	var haveRoot, haveID bool
	rest := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		tok := args[i]
		if tok == flagTerminator {
			// Preserve normal argv semantics: from the terminator on, every
			// token stays positional and untouched.
			rest = append(rest, args[i:]...)
			return finish(root, id, haveRoot, haveID, rest)
		}
		if strings.HasPrefix(tok, FlagInstallRoot+"=") || strings.HasPrefix(tok, FlagInstallID+"=") {
			return Locators{}, false, nil, fmt.Errorf("%w: %q must use the separate-value form", ErrMalformed, tok)
		}
		switch tok {
		case FlagInstallRoot, FlagInstallID:
			if i+1 >= len(args) {
				return Locators{}, false, nil, fmt.Errorf("%w: %s requires a value", ErrMalformed, tok)
			}
			value := args[i+1]
			i++
			if value == "" || strings.HasPrefix(value, "-") {
				return Locators{}, false, nil, fmt.Errorf("%w: %s value must not be empty or flag-like", ErrMalformed, tok)
			}
			switch tok {
			case FlagInstallRoot:
				if haveRoot {
					return Locators{}, false, nil, fmt.Errorf("%w: duplicate %s", ErrMalformed, FlagInstallRoot)
				}
				root, haveRoot = value, true
			case FlagInstallID:
				if haveID {
					return Locators{}, false, nil, fmt.Errorf("%w: duplicate %s", ErrMalformed, FlagInstallID)
				}
				id, haveID = value, true
			}
		default:
			rest = append(rest, tok)
		}
	}
	return finish(root, id, haveRoot, haveID, rest)
}

// finish applies the pair and canonical-value rules once scanning is done.
func finish(root, id string, haveRoot, haveID bool, remaining []string) (Locators, bool, []string, error) {
	if haveRoot != haveID {
		return Locators{}, false, nil, fmt.Errorf("%w: both locator flags are required together", ErrMalformed)
	}
	if !haveRoot {
		return Locators{}, false, remaining, nil
	}
	locators := Locators{InstallRoot: root, InstallID: id}
	if err := locators.Validate(); err != nil {
		return Locators{}, false, nil, err
	}
	return locators, true, remaining, nil
}

// LocatorArgs returns the exact approved private argv tokens a trusted fixed
// launcher appends to a child command line. It emits ONLY the two locator
// flags and their validated values — never a digest, an executable path, or
// any other authority. Round-trips through Parse.
func LocatorArgs(installRoot, installID string) ([]string, error) {
	locators := Locators{InstallRoot: installRoot, InstallID: installID}
	if err := locators.Validate(); err != nil {
		return nil, err
	}
	return []string{FlagInstallRoot, installRoot, FlagInstallID, installID}, nil
}
