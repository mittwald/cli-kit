package selfupdate

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
)

// Release is one published version of a tool.
type Release struct {
	// Tag is the release's tag, such as "v1.2.3".
	Tag string
	// Assets are the files the source lists for the release. A source that
	// derives download URLs from the asset name, such as [GitLab], leaves it
	// empty.
	Assets []Asset
}

// Version returns the tag without a leading "v" or "V": the value
// GoReleaser's {{ .Version }} template has.
func (r Release) Version() string {
	return strings.TrimPrefix(strings.TrimPrefix(r.Tag, "v"), "V")
}

// Asset is one downloadable file of a release.
type Asset struct {
	// Name is the file name, such as "tool_Linux_x86_64.tar.gz".
	Name string
	// URL is where the source downloads it from.
	URL string
}

// Source finds releases and downloads their files.
type Source interface {
	// Latest returns the most recent stable release.
	Latest(ctx context.Context) (Release, error)
	// Download opens the named file of release rel. It returns an error
	// wrapping [ErrAssetNotFound] when the release has no such file. The
	// caller closes the reader.
	Download(ctx context.Context, rel Release, name string) (io.ReadCloser, error)
}

// TokenFunc returns the credential for a request. It is called with retry
// false before the first request, and once more with retry true when the
// server rejected the credential (401 or 403), so it can drop a cached token
// and ask for a fresh one. Returning an error stops the request. A nil
// TokenFunc sends no credential.
type TokenFunc func(ctx context.Context, retry bool) (string, error)

var (
	// ErrNoRelease reports that the source has no published release.
	ErrNoRelease = errors.New("selfupdate: no release found")
	// ErrAssetNotFound reports that a release has no file of the requested
	// name.
	ErrAssetNotFound = errors.New("selfupdate: release asset not found")
	// ErrUnauthorized reports that the server rejected the credential, or
	// that it requires one and none was given.
	ErrUnauthorized = errors.New("selfupdate: unauthorized")
)

// defaultUserAgent identifies requests; GitHub rejects requests without one.
const defaultUserAgent = "cli-kit-selfupdate"

// requester sends authenticated GET requests, retrying once with a fresh
// credential when the first one is rejected.
type requester struct {
	client    *http.Client
	token     TokenFunc
	userAgent string
	// headers are set on every request.
	headers map[string]string
	// authHeader is the header the credential goes in. authScheme, when
	// set, precedes the credential, as in "Bearer <token>".
	authHeader string
	authScheme string
}

// get sends a GET request and returns the response when its status is 200.
// A 404 becomes an error wrapping notFound.
func (r requester) get(ctx context.Context, url, accept string, notFound error) (*http.Response, error) {
	token, err := r.credential(ctx, false)
	if err != nil {
		return nil, err
	}
	resp, err := r.send(ctx, url, accept, token)
	if err != nil {
		return nil, err
	}
	if (resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden) && r.token != nil {
		drain(resp)
		token, err = r.credential(ctx, true)
		if err != nil {
			return nil, err
		}
		if resp, err = r.send(ctx, url, accept, token); err != nil {
			return nil, err
		}
	}
	if resp.StatusCode == http.StatusOK {
		return resp, nil
	}
	defer drain(resp)
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	detail := strings.TrimSpace(string(body))
	if token != "" {
		detail = strings.ReplaceAll(detail, token, "[REDACTED]")
	}
	statusErr := fmt.Errorf("GET %s: %s", url, resp.Status)
	if detail != "" {
		statusErr = fmt.Errorf("GET %s: %s: %s", url, resp.Status, detail)
	}
	switch resp.StatusCode {
	case http.StatusNotFound:
		return nil, fmt.Errorf("%w: %w", notFound, statusErr)
	case http.StatusUnauthorized, http.StatusForbidden:
		return nil, fmt.Errorf("%w: %w", ErrUnauthorized, statusErr)
	default:
		return nil, fmt.Errorf("selfupdate: %w", statusErr)
	}
}

func (r requester) credential(ctx context.Context, retry bool) (string, error) {
	if r.token == nil {
		return "", nil
	}
	token, err := r.token(ctx, retry)
	if err != nil {
		return "", fmt.Errorf("selfupdate: credential: %w", err)
	}
	return strings.TrimSpace(token), nil
}

func (r requester) send(ctx context.Context, url, accept, token string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("selfupdate: %w", err)
	}
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	userAgent := r.userAgent
	if userAgent == "" {
		userAgent = defaultUserAgent
	}
	req.Header.Set("User-Agent", userAgent)
	for name, value := range r.headers {
		req.Header.Set(name, value)
	}
	client := r.client
	if client == nil {
		client = http.DefaultClient
	}
	if token != "" && r.authHeader != "" {
		value := token
		if r.authScheme != "" {
			value = r.authScheme + " " + token
		}
		req.Header.Set(r.authHeader, value)
		client = withoutCredentialOnRedirect(client, r.authHeader)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("selfupdate: %w", err)
	}
	return resp, nil
}

// withoutCredentialOnRedirect returns a copy of client that removes header
// from a redirected request once the redirects left the origin (scheme,
// host and port) of the first request. The HTTP client copies all headers
// of the first request to each redirect and only drops Authorization and
// cookies on its own, so a GitLab token header would otherwise reach the
// storage host a package download redirects to. The client's own
// CheckRedirect still applies.
func withoutCredentialOnRedirect(client *http.Client, header string) *http.Client {
	c := *client
	next := client.CheckRedirect
	c.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		left := !sameOrigin(req.URL, via[0].URL)
		for _, prev := range via[1:] {
			left = left || !sameOrigin(prev.URL, via[0].URL)
		}
		if left {
			req.Header.Del(header)
		}
		if next != nil {
			return next(req, via)
		}
		if len(via) >= 10 {
			return errors.New("stopped after 10 redirects")
		}
		return nil
	}
	return &c
}

func sameOrigin(a, b *url.URL) bool {
	return strings.EqualFold(a.Scheme, b.Scheme) && strings.EqualFold(hostPort(a), hostPort(b))
}

// hostPort returns the URL's host with the scheme's default port made
// explicit, so that "example.com" and "example.com:443" compare equal.
func hostPort(u *url.URL) string {
	port := u.Port()
	if port == "" {
		switch strings.ToLower(u.Scheme) {
		case "https":
			port = "443"
		case "http":
			port = "80"
		}
	}
	return net.JoinHostPort(u.Hostname(), port)
}

func drain(resp *http.Response) {
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	_ = resp.Body.Close()
}
