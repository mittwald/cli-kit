package buildinfo

import (
	"fmt"
	"runtime"
	"runtime/debug"
	"strings"
	"time"
)

// Values stamped at link time with -ldflags "-X …". Leave them empty in the
// source; [Get] falls back to the toolchain's build information for every
// value that was not stamped.
var (
	// Version is the release version, with or without a leading "v".
	Version string
	// Commit is the full VCS revision the binary was built from.
	Commit string
	// Date is the build or commit time in RFC 3339 format.
	Date string
)

// DevVersion is the version a build without any version information reports.
const DevVersion = "dev"

// shortCommitLength is how many characters of a revision [Info.String] shows.
const shortCommitLength = 12

// Info describes one build of a command.
type Info struct {
	// Version is the release version as stamped or recorded, such as "v1.2.3"
	// or "1.2.3", or [DevVersion] when nothing is known.
	Version string
	// Commit is the full VCS revision, or "" when unknown.
	Commit string
	// Date is the build or commit time, or the zero time when unknown.
	Date time.Time
	// Modified reports whether the working tree had uncommitted changes when
	// the binary was built. Only the toolchain records it.
	Modified bool
	// GoVersion is the Go release the binary was built with.
	GoVersion string
}

// readBuildInfo is debug.ReadBuildInfo, replaced in tests.
var readBuildInfo = debug.ReadBuildInfo

// Get returns the build information of the running binary: the values
// stamped through linker flags, completed from the toolchain's records.
func Get() Info {
	return get(Version, Commit, Date, readBuildInfo)
}

func get(version, commit, date string, read func() (*debug.BuildInfo, bool)) Info {
	info := Info{
		Version:   strings.TrimSpace(version),
		Commit:    strings.TrimSpace(commit),
		GoVersion: runtime.Version(),
	}
	if t, err := time.Parse(time.RFC3339, strings.TrimSpace(date)); err == nil {
		info.Date = t
	}

	if build, ok := read(); ok && build != nil {
		if build.GoVersion != "" {
			info.GoVersion = build.GoVersion
		}
		if info.Version == "" || info.Version == DevVersion {
			if v := build.Main.Version; v != "" && v != "(devel)" {
				info.Version = v
			}
		}
		for _, setting := range build.Settings {
			switch setting.Key {
			case "vcs.revision":
				if info.Commit == "" {
					info.Commit = setting.Value
				}
			case "vcs.time":
				if info.Date.IsZero() {
					if t, err := time.Parse(time.RFC3339, setting.Value); err == nil {
						info.Date = t
					}
				}
			case "vcs.modified":
				info.Modified = setting.Value == "true"
			}
		}
	}

	if info.Version == "" {
		info.Version = DevVersion
	}
	return info
}

// ShortCommit returns the first 12 characters of the revision, or "" when it
// is unknown.
func (i Info) ShortCommit() string {
	if len(i.Commit) > shortCommitLength {
		return i.Commit[:shortCommitLength]
	}
	return i.Commit
}

// String returns the version as shown to users. A version with a leading
// "v" before a digit loses it ("v1.4.0" becomes "1.4.0"); a development build
// carries its short revision and, when the tree was dirty, a ".dirty" marker
// ("dev+0123456789ab.dirty").
func (i Info) String() string {
	v := strings.TrimSpace(i.Version)
	if v == "" {
		v = DevVersion
	}
	if v == DevVersion {
		if revision := i.ShortCommit(); revision != "" {
			v += "+" + revision
			if i.Modified {
				v += ".dirty"
			}
		}
		return v
	}
	if len(v) > 1 && (v[0] == 'v' || v[0] == 'V') && v[1] >= '0' && v[1] <= '9' {
		return v[1:]
	}
	return v
}

// Detail returns a multi-line description for a version command: the
// version line, then the revision, build date and Go release where known.
// name is the command's name, written in front of the version.
func (i Info) Detail(name string) string {
	var b strings.Builder
	if name != "" {
		b.WriteString(name)
		b.WriteByte(' ')
	}
	b.WriteString(i.String())
	b.WriteByte('\n')
	if i.Commit != "" {
		commit := i.Commit
		if i.Modified {
			commit += " (modified)"
		}
		fmt.Fprintf(&b, "commit: %s\n", commit)
	}
	if !i.Date.IsZero() {
		fmt.Fprintf(&b, "built:  %s\n", i.Date.UTC().Format(time.RFC3339))
	}
	if i.GoVersion != "" {
		fmt.Fprintf(&b, "go:     %s\n", i.GoVersion)
	}
	return b.String()
}
