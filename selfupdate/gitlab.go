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
	// projects.
	Token TokenFunc
	// TokenHeader is the header the credential goes in; empty means
	// [HeaderPrivateToken].
	TokenHeader string
	// Client sends the requests; nil means http.DefaultClient. A redirect
	// to another origin (scheme, host and port) never carries the
	// credential; the client's own CheckRedirect still applies.
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

func (g *GitLab) requester() requester {
	header := g.TokenHeader
	if header == "" {
		header = HeaderPrivateToken
	}
	r := requester{
		client:     g.Client,
		token:      g.Token,
		userAgent:  g.UserAgent,
		authHeader: header,
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

// Latest implements Source.
func (g *GitLab) Latest(ctx context.Context) (Release, error) {
	if err := g.check(); err != nil {
		return Release{}, err
	}
	resp, err := g.requester().get(ctx, g.api()+"/projects/"+g.project()+"/releases/permalink/latest",
		"application/json", ErrNoRelease)
	if err != nil {
		return Release{}, err
	}
	defer drain(resp)
	var body struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&body); err != nil {
		return Release{}, fmt.Errorf("selfupdate: decode latest release: %w", err)
	}
	if body.TagName == "" {
		return Release{}, ErrNoRelease
	}
	return Release{Tag: body.TagName}, nil
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
	var lastErr error
	for _, version := range versions {
		resp, err := g.requester().get(ctx, g.api()+"/projects/"+g.project()+"/packages/generic/"+
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
		lastErr, strings.Join(versions, " and "))
}
