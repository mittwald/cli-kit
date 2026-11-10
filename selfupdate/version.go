package selfupdate

import (
	"strconv"
	"strings"
)

// version is a parsed semantic version: numeric core parts and the
// dot-separated pre-release identifiers. Build metadata is dropped, as
// semantic versioning ignores it for precedence.
type version struct {
	core []int
	pre  []string
}

// parseVersion reads "v1.2.3", "1.2", "1.2.3-rc.1+build.5" and the like.
// It reports false for anything else, such as "dev" or a commit hash.
func parseVersion(s string) (version, bool) {
	s = strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(s), "v"), "V")
	s, _, _ = strings.Cut(s, "+")
	core, pre, hasPre := strings.Cut(s, "-")
	if core == "" {
		return version{}, false
	}
	var v version
	for part := range strings.SplitSeq(core, ".") {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return version{}, false
		}
		v.core = append(v.core, n)
	}
	if hasPre {
		if pre == "" {
			return version{}, false
		}
		v.pre = strings.Split(pre, ".")
	}
	return v, true
}

// Compare orders two version strings by semantic version precedence:
// -1 when a is older than b, 0 when they are equal, +1 when a is newer. A
// leading "v" is ignored, missing parts count as 0 ("1.2" equals "1.2.0"),
// and a pre-release is older than its release. A string that is no version
// at all ("dev", "") is older than any version; two such strings compare
// as text.
func Compare(a, b string) int {
	va, okA := parseVersion(a)
	vb, okB := parseVersion(b)
	switch {
	case !okA && !okB:
		return strings.Compare(a, b)
	case !okA:
		return -1
	case !okB:
		return 1
	}
	for i := 0; i < max(len(va.core), len(vb.core)); i++ {
		x, y := part(va.core, i), part(vb.core, i)
		if x != y {
			return sign(x - y)
		}
	}
	switch {
	case len(va.pre) == 0 && len(vb.pre) == 0:
		return 0
	case len(va.pre) == 0:
		return 1
	case len(vb.pre) == 0:
		return -1
	}
	for i := 0; i < min(len(va.pre), len(vb.pre)); i++ {
		if c := comparePre(va.pre[i], vb.pre[i]); c != 0 {
			return c
		}
	}
	return sign(len(va.pre) - len(vb.pre))
}

// IsNewer reports whether latest is a newer version than current. A
// current that is no version at all, such as "dev" from a local build,
// counts as older than any release.
func IsNewer(latest, current string) bool { return Compare(latest, current) > 0 }

func part(parts []int, i int) int {
	if i < len(parts) {
		return parts[i]
	}
	return 0
}

// comparePre compares pre-release identifiers: numeric ones numerically,
// and below alphanumeric ones, which compare as text.
func comparePre(a, b string) int {
	na, errA := strconv.Atoi(a)
	nb, errB := strconv.Atoi(b)
	switch {
	case errA == nil && errB == nil:
		return sign(na - nb)
	case errA == nil:
		return -1
	case errB == nil:
		return 1
	default:
		return strings.Compare(a, b)
	}
}

func sign(n int) int {
	switch {
	case n < 0:
		return -1
	case n > 0:
		return 1
	}
	return 0
}
