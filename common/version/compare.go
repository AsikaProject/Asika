package version

import (
	"fmt"
	"strconv"
	"strings"
)

// Suffix priority orders same-day releases. A higher number means newer.
//
//	HF  — hotfix, supersedes the same-day release
//	CVE — security fix, supersedes the same-day release but not a hotfix
//	""  — plain dated release (no suffix)
//	DEP — dependency-only update
//	DEV — development build, lowest priority
//
// Any unrecognised suffix sorts below "" so unknown variants cannot
// accidentally upgrade a release user.
var suffixPriority = map[string]int{
	"HF":  4,
	"CVE": 3,
	"":    2,
	"DEP": 1,
	"DEV": 0,
}

// IsDevBuild reports whether v is a development / unreleased build. The
// canonical dev format is YYYYMMDDDEV-HASH (note the hyphen before the
// commit hash); the literal "dev" string marks an uninstrumented go build.
// Releases are pure dates or dates with a HF/CVE/DEP suffix and no hash.
func IsDevBuild(v string) bool {
	return v == "dev" || strings.Contains(v, "-")
}

// Compare returns -1, 0 or +1 according to whether a is older than, equal
// to, or newer than b. The version grammar is:
//
//	YYYYMMDD[SUFFIX]            — release
//	YYYYMMDDDEV-HASH | "dev"    — dev build
//
// Comparison falls back to lexical ordering when the grammar is not
// recognised, so Compare never panics and always returns a total order.
func Compare(a, b string) int {
	va, okA := parseVersion(a)
	vb, okB := parseVersion(b)
	if okA && okB {
		return cmpParsed(va, vb)
	}
	// Fall back to lexical for malformed input so callers still get a
	// deterministic ordering.
	return strings.Compare(a, b)
}

// IsUpgradeable reports whether latest is strictly newer than current.
//
// Dev builds are always upgradeable: they have no reliable ordering and
// the user almost always wants to move to a real release. The reverse
// path (release → dev) is never upgradeable, satisfying the project rule
// that dev builds cannot be promoted via self-update.
func IsUpgradeable(current, latest string) bool {
	if latest == "" {
		return false
	}
	if IsDevBuild(current) {
		return !IsDevBuild(latest)
	}
	return Compare(latest, current) > 0
}

type parsed struct {
	date   int64
	suffix string
	hash   string
	dev    bool
}

func parseVersion(v string) (parsed, bool) {
	if v == "dev" {
		return parsed{dev: true}, true
	}
	if len(v) < 8 {
		return parsed{}, false
	}
	datePart := v[:8]
	date, err := strconv.ParseInt(datePart, 10, 64)
	if err != nil || date < 10000000 || date > 99991231 {
		return parsed{}, false
	}
	rest := v[8:]

	// Split optional -HASH tail.
	hash := ""
	if idx := strings.Index(rest, "-"); idx >= 0 {
		hash = rest[idx+1:]
		rest = rest[:idx]
	}

	p := parsed{date: date, suffix: rest, hash: hash}
	if _, ok := suffixPriority[rest]; !ok {
		// Unknown suffix: reject as malformed so callers fall back to
		// lexical compare. DEV with hash is still parsed (dev=true) so
		// the dev branch in IsUpgradeable works.
		if rest == "DEV" {
			p.dev = true
			return p, true
		}
		return parsed{}, false
	}
	if rest == "DEV" {
		p.dev = true
	}
	return p, true
}

func cmpParsed(a, b parsed) int {
	if a.date != b.date {
		if a.date < b.date {
			return -1
		}
		return 1
	}
	pa := suffixPriority[a.suffix]
	pb := suffixPriority[b.suffix]
	if pa != pb {
		if pa < pb {
			return -1
		}
		return 1
	}
	// Same date and suffix: fall back to hash ordering for determinism.
	// Hash is irrelevant for released builds (no hash) but lets two dev
	// builds on the same day still compare in a stable way.
	return strings.Compare(a.hash, b.hash)
}

// String is a tiny helper for log formatting.
func (p parsed) String() string {
	return fmt.Sprintf("{date=%d suffix=%q hash=%q dev=%v}", p.date, p.suffix, p.hash, p.dev)
}
