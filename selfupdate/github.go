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

// GitHub is a Source for GitHub Releases. The latest release is the most
// recent one that is neither a draft nor a pre-release.
type GitHub struct {
	// Owner and Repo name the repository, such as "example" and "tool".
	Owner string
	Repo  string
	// Token supplies the credential; nil sends none, which suits public
	// repositories within the unauthenticated rate limit.
	Token TokenFunc
	// APIURL is the API root; empty means https://api.github.com. Set it
	// for GitHub Enterprise Server ("https://github.example.com/api/v3") or
	// tests.
	APIURL string
	// Client sends the requests; nil means http.DefaultClient. A redirect
	// to another origin (scheme, host and port) never carries the
	// credential; the client's own CheckRedirect still applies.
	Client *http.Client
	// UserAgent identifies the requests; empty means a generic default.
	UserAgent string
}

func (g *GitHub) api() string {
	if g.APIURL == "" {
		return "https://api.github.com"
	}
	return strings.TrimRight(g.APIURL, "/")
}

func (g *GitHub) requester() requester {
	return requester{
		client:     g.Client,
		token:      g.Token,
		userAgent:  g.UserAgent,
		headers:    map[string]string{"X-GitHub-Api-Version": "2022-11-28"},
		authHeader: "Authorization",
		authScheme: "Bearer",
	}
}

// Latest implements Source.
func (g *GitHub) Latest(ctx context.Context) (Release, error) {
	if g.Owner == "" || g.Repo == "" {
		return Release{}, errors.New("selfupdate: GitHub.Owner and GitHub.Repo are required")
	}
	resp, err := g.requester().get(ctx, g.api()+"/repos/"+url.PathEscape(g.Owner)+"/"+url.PathEscape(g.Repo)+"/releases/latest",
		"application/vnd.github+json", ErrNoRelease)
	if err != nil {
		return Release{}, err
	}
	defer drain(resp)
	var body struct {
		TagName string `json:"tag_name"`
		Assets  []struct {
			Name string `json:"name"`
			URL  string `json:"url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(&body); err != nil {
		return Release{}, fmt.Errorf("selfupdate: decode latest release: %w", err)
	}
	if body.TagName == "" {
		return Release{}, ErrNoRelease
	}
	rel := Release{Tag: body.TagName}
	for _, a := range body.Assets {
		rel.Assets = append(rel.Assets, Asset{Name: a.Name, URL: a.URL})
	}
	return rel, nil
}

// Download implements Source. It downloads through the asset's API URL,
// which works for private repositories too; the redirect to the storage
// host does not carry the credential along.
func (g *GitHub) Download(ctx context.Context, rel Release, name string) (io.ReadCloser, error) {
	for _, a := range rel.Assets {
		if a.Name != name {
			continue
		}
		resp, err := g.requester().get(ctx, a.URL, "application/octet-stream", ErrAssetNotFound)
		if err != nil {
			return nil, err
		}
		return resp.Body, nil
	}
	return nil, fmt.Errorf("%w: release %s has no file %s", ErrAssetNotFound, rel.Tag, name)
}
