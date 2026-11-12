package selfupdate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// Header names GitLab accepts a credential in.
const (
	// HeaderPrivateToken carries a personal, project or group access token.
	HeaderPrivateToken = "PRIVATE-TOKEN"
	// HeaderJobToken carries a CI/CD job token ($CI_JOB_TOKEN).
	HeaderJobToken = "JOB-TOKEN"
	// HeaderDeployToken carries a deploy token with read_package_registry.
	HeaderDeployToken = "DEPLOY-TOKEN"
	// HeaderAuthorization sends the token as "Authorization: Bearer", as
	// OAuth tokens need.
	HeaderAuthorization = "Authorization"
)

// GitLab is a Source for a GitLab project. The latest release comes from
// the project's Releases API; files come from its Generic Package Registry,
// as GoReleaser publishes them with gitlab_urls.use_package_registry. Plain
// release links are not used: on many instances they only resolve through
// a browser session, whatever token the request carries.
//
// GitLab has no notion of a pre-release, so the latest release is found by
// its tag: the highest stable version among the project's most recent
// releases. A tag with a pre-release part, such as "v2.0.0-rc.1", and a
// tag that is no version are skipped.
type GitLab struct {
	// BaseURL is the instance, such as "https://gitlab.example.com". An URL
	// ending in "/api/v4" is taken as the API root itself.
	BaseURL string
	// Project is the project's path, such as "example/tool", or its numeric
	// ID. It is URL-encoded as the API requires; an already encoded path
	// (containing "%2F") is used as it is.
	Project string
	// Package is the Generic Package Registry package the releases are
	// published to; GoReleaser uses the project name.
	Package string
	// Token supplies the credential; nil sends none, which suits public
	// projects. With a Token, BaseURL has to be an https URL, or an http
	// URL of a loopback address; see [TokenFunc].
	Token TokenFunc
	// TokenHeader is the header the credential goes in; empty means
	// [HeaderPrivateToken].
	TokenHeader string
	// Client sends the requests. Nil means a client of this package's own
	// that gives up on a server that does not connect, finish the TLS
	// handshake or send response headers within half a minute, and on a
	// response body that delivers nothing for a minute; a whole request has
	// no time limit, as a download may take long. A Client set here is used
	// as it is: its timeouts, and the context's deadline, are the caller's
	// to set. A redirect to another origin (scheme, host and port) never
	// carries the credential; the client's own CheckRedirect still applies.
	Client *http.Client
	// UserAgent identifies the requests; empty means a generic default.
	UserAgent string
}

func (g *GitLab) api() string {
	base := strings.TrimRight(g.BaseURL, "/")
	if strings.HasSuffix(base, "/api/v4") {
		return base
	}
	return base + "/api/v4"
}

func (g *GitLab) project() string {
	if strings.Contains(g.Project, "%") {
		return g.Project
	}
	return url.PathEscape(strings.Trim(g.Project, "/"))
}

func (g *GitLab) requester() *requester {
	header := g.TokenHeader
	if header == "" {
		header = HeaderPrivateToken
	}
	r := &requester{
		client:     g.Client,
		token:      g.Token,
		userAgent:  g.UserAgent,
		authHeader: header,
		origin:     g.api(),
	}
	if http.CanonicalHeaderKey(header) == HeaderAuthorization {
		r.authScheme = "Bearer"
	}
	return r
}

func (g *GitLab) check() error {
	if g.BaseURL == "" || g.Project == "" {
		return errors.New("selfupdate: GitLab.BaseURL and GitLab.Project are required")
	}
	return nil
}

// The releases GitLab.Latest looks at: pages of gitLabReleasesPerPage,
// newest first, until one holds a stable version, and no more than
// gitLabReleasePages of them.
const (
	gitLabReleasesPerPage = 20
	gitLabReleasePages    = 5
)

// Latest implements Source. It returns the highest stable version among
// the project's most recent releases: it reads the releases newest first,
// twenty at a time, takes the highest stable version of the first page
// that holds one, and gives up with [ErrNoRelease] after a hundred
// releases without one.
func (g *GitLab) Latest(ctx context.Context) (Release, error) {
	if err := g.check(); err != nil {
		return Release{}, err
	}
	r := g.requester()
	seen := 0
	for page := 1; page <= gitLabReleasePages; page++ {
		tags, err := g.releaseTags(ctx, r, page)
		if err != nil {
			return Release{}, err
		}
		best := ""
		for _, tag := range tags {
			if stableVersion(tag) && (best == "" || Compare(tag, best) > 0) {
				best = tag
			}
		}
		if best != "" {
			return Release{Tag: best}, nil
		}
		seen += len(tags)
		if len(tags) < gitLabReleasesPerPage {
			break
		}
	}
	if seen == 0 {
		return Release{}, ErrNoRelease
	}
	return Release{}, fmt.Errorf("%w: none of the %d most recent releases has a stable version as its tag", ErrNoRelease, seen)
}

// releaseTags returns the tags of one page of the project's releases,
// newest first.
func (g *GitLab) releaseTags(ctx context.Context, r *requester, page int) ([]string, error) {
	resp, err := r.get(ctx, fmt.Sprintf("%s/projects/%s/releases?order_by=released_at&sort=desc&per_page=%d&page=%d",
		g.api(), g.project(), gitLabReleasesPerPage, page), "application/json", ErrNoRelease)
	if err != nil {
		return nil, err
	}
	defer drain(resp)
	var body []struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(&body); err != nil {
		return nil, fmt.Errorf("selfupdate: decode releases: %w", err)
	}
	tags := make([]string, 0, len(body))
	for _, release := range body {
		tags = append(tags, release.TagName)
	}
	return tags, nil
}

// Download implements Source. GoReleaser publishes a package under its
// {{ .Version }} (the tag without a leading "v"), so that version is tried
// first, then the raw tag.
func (g *GitLab) Download(ctx context.Context, rel Release, name string) (io.ReadCloser, error) {
	if err := g.check(); err != nil {
		return nil, err
	}
	if g.Package == "" {
		return nil, errors.New("selfupdate: GitLab.Package is required")
	}
	versions := []string{rel.Version()}
	if rel.Tag != rel.Version() {
		versions = append(versions, rel.Tag)
	}
	// One requester for both attempts, so a TokenFunc without a credential
	// is asked again after a 404 once, not once per attempt.
	r := g.requester()
	var lastErr error
	for _, version := range versions {
		resp, err := r.get(ctx, g.api()+"/projects/"+g.project()+"/packages/generic/"+
			url.PathEscape(g.Package)+"/"+url.PathEscape(version)+"/"+url.PathEscape(name), "", ErrAssetNotFound)
		if err == nil {
			return resp.Body, nil
		}
		if !errors.Is(err, ErrAssetNotFound) {
			return nil, err
		}
		lastErr = err
	}
	return nil, fmt.Errorf("%w (tried package versions %s; check that the release was published to the Generic Package Registry)",
		lastErr, plain(strings.Join(versions, " and ")))
}
