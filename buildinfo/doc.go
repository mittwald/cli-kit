// Package buildinfo reports which build of a command is running.
//
// Release builds stamp the version, commit and build date through linker
// flags. With GoReleaser:
//
//	builds:
//	  - ldflags:
//	      - -s -w
//	      - -X github.com/mittwald/cli-kit/buildinfo.Version={{ .Version }}
//	      - -X github.com/mittwald/cli-kit/buildinfo.Commit={{ .FullCommit }}
//	      - -X github.com/mittwald/cli-kit/buildinfo.Date={{ .Date }}
//
// and with a plain go build:
//
//	go build -ldflags "-X github.com/mittwald/cli-kit/buildinfo.Version=1.2.3 \
//	  -X github.com/mittwald/cli-kit/buildinfo.Commit=$(git rev-parse HEAD)"
//
// Builds without linker flags fall back to what the Go toolchain records in
// the binary: the module version for "go install example.com/tool@v1.2.3",
// the version Go 1.24 and later derive from the repository's tags for a plain
// "go build" of a tagged commit, and the VCS revision, commit time and dirty
// state. Any other build is a development build: [Info.String] reports it as
// "dev" with its revision, such as "dev+0123456789ab.dirty", rather than the
// pseudo-version the toolchain made up for it.
package buildinfo
