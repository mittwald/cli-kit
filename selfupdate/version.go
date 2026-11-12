package selfupdate

import (
	"errors"
	"strconv"
	"strings"
)

// ErrNoVersion reports that the latest release's tag is no version, such
// as "release-2" or "nightly", so nothing can tell whether it is newer
// than the running one.
var ErrNoVersion = errors.New("selfupdate: release tag is not a version")

// version is a parsed semantic version: numeric core parts and the
// dot-separated pre-release identifiers. Build metadata is dropped, as
// semantic versioning ignores it for precedence.
type version struct {
	core []int
	pre  []string
	// ahead and dirty come from the suffix "git describe" appends to a tag
	// for a build that is not the tagged commit: the number of commits
	// since the tag, and whether the work tree was modified.
	ahead int
	dirty bool
}

// stable reports whether v is a release proper: no pre-release, and not a
// build of some commit after one.
func (v version) stable() bool { return len(v.pre) == 0 && v.ahead == 0 && !v.dirty }

// stableVersion reports whether tag is a version without a pre-release
// part.
func stableVersion(tag string) bool {
	v, ok := parseVersion(tag)
	return ok && v.stable()
}

// parseVersion reads "v1.2.3", "1.2", "1.2.3-rc.1+build.5" and the like,
// and what "git describe" makes of such a tag: "v1.2.3-5-gabc1234",
// "v1.2.3-dirty". It reports false for anything else, such as "dev" or a
// commit hash.
func parseVersion(s string) (version, bool) {
	s = strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(s), "v"), "V")
	s, _, _ = strings.Cut(s, "+")
	var v version
	s, v.ahead, v.dirty = cutDescribe(s)
	core, pre, hasPre := strings.Cut(s, "-")
	if core == "" {
		return version{}, false
	}
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

// cutDescribe removes the suffix "git describe" appends to a tag:
// "-<commits>-g<abbreviated hash>" for a commit after the tag, and
// "-dirty" for a modified work tree. It returns the tag, the number of
// commits and whether the tree was dirty.
func cutDescribe(s string) (tag string, ahead int, dirty bool) {
	tag, dirty = strings.CutSuffix(s, "-dirty")
	hashAt := strings.LastIndex(tag, "-g")
	if hashAt < 0 || !abbreviatedHash(tag[hashAt+2:]) {
		return tag, 0, dirty
	}
	countAt := strings.LastIndexByte(tag[:hashAt], '-')
	if countAt < 0 {
		return tag, 0, dirty
	}
	count := tag[countAt+1 : hashAt]
	if count == "" || strings.Trim(count, "0123456789") != "" {
		return tag, 0, dirty
	}
	n, err := strconv.Atoi(count)
	if err != nil {
		return tag, 0, dirty
	}
	return tag[:countAt], n, dirty
}

// abbreviatedHash reports whether s can be an abbreviated commit hash:
// four to sixty-four lower-case hexadecimal digits.
func abbreviatedHash(s string) bool {
	return len(s) >= 4 && len(s) <= 64 && strings.Trim(s, "0123456789abcdef") == ""
}

// Compare orders two version strings by semantic version precedence:
// -1 when a is older than b, 0 when they are equal, +1 when a is newer. A
// leading "v" is ignored, missing parts count as 0 ("1.2" equals "1.2.0"),
// and a pre-release is older than its release. A string that is no version
// at all ("dev", "") is older than any version; two such strings compare
// as text.
//
// What "git describe" prints for a build that is not a tagged commit is a
// version too: "v1.2.3-5-gabc1234", five commits after v1.2.3, and
// "v1.2.3-dirty", built from a modified work tree, are both newer than
// v1.2.3 and older than the next version. Semantic versioning alone would
// read the suffix as a pre-release and rank such a build below its tag.
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
	if c := comparePreRelease(va.pre, vb.pre); c != 0 {
		return c
	}
	if va.ahead != vb.ahead {
		return sign(va.ahead - vb.ahead)
	}
	switch {
	case va.dirty == vb.dirty:
		return 0
	case va.dirty:
		return 1
	}
	return -1
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

// comparePreRelease compares the pre-release identifiers of two versions
// with equal core parts. A version without any is the release, and newer.
func comparePreRelease(a, b []string) int {
	switch {
	case len(a) == 0 && len(b) == 0:
		return 0
	case len(a) == 0:
		return 1
	case len(b) == 0:
		return -1
	}
	for i := 0; i < min(len(a), len(b)); i++ {
		if c := comparePre(a[i], b[i]); c != 0 {
			return c
		}
	}
	return sign(len(a) - len(b))
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
