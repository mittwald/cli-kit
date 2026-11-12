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
	"sync/atomic"
	"time"
	"unicode"
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
// server rejected the credential (401, or a 403 that is no rate limit), so
// it can drop a cached token and ask for a fresh one. It is also called
// with retry true when it returned no credential and the server answered
// 404, as a private project does to an anonymous request; an error or an
// empty credential from that call leaves the 404 standing. A rate limit
// (see [ErrRateLimited]) never asks again, as the credential is not at
// fault. Returning an error stops the request. A nil TokenFunc sends no
// credential.
//
// The credential is only ever sent to the origin (scheme, host and port)
// of the source's API, and never over plain http, except to a loopback
// address: such a request fails before it is sent.
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
	// ErrRateLimited reports that the server refused the request because
	// too many were sent: a 429, or a 403 that carries a Retry-After header
	// or a rate-limit header saying that no requests remain. The credential
	// is not at fault, so the [TokenFunc] is not asked for another one, and
	// the error never matches [ErrUnauthorized].
	ErrRateLimited = errors.New("selfupdate: rate limited")
)

// defaultUserAgent identifies requests; GitHub rejects requests without one.
const defaultUserAgent = "cli-kit-selfupdate"

// Timeouts of the client a source uses when it is given none. There is no
// timeout for a whole request, as a download may take long; a response
// body that delivers nothing for defaultStallTimeout is given up instead.
const (
	defaultDialTimeout           = 30 * time.Second
	defaultTLSHandshakeTimeout   = 15 * time.Second
	defaultResponseHeaderTimeout = 30 * time.Second
	defaultStallTimeout          = time.Minute
)

// defaultClient sends the requests of a source that has no Client.
var defaultClient = newClient(defaultResponseHeaderTimeout)

// newClient returns a client that gives up on a server that does not
// accept the connection, finish the TLS handshake or answer with response
// headers in time.
func newClient(responseHeaderTimeout time.Duration) *http.Client {
	return &http.Client{Transport: &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: defaultDialTimeout, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:     true,
		TLSHandshakeTimeout:   defaultTLSHandshakeTimeout,
		ResponseHeaderTimeout: responseHeaderTimeout,
		ExpectContinueTimeout: time.Second,
		MaxIdleConns:          10,
		IdleConnTimeout:       90 * time.Second,
	}}
}

// errStalled reports a response body that stopped arriving.
var errStalled = errors.New("selfupdate: the server stopped sending data")

// stallBody is a response body whose Read gives up, by canceling the
// request, when no data arrives within timeout. Only the time spent
// waiting in Read counts, not the time the reader takes between calls.
type stallBody struct {
	body    io.ReadCloser
	timeout time.Duration
	timer   *time.Timer
	cancel  context.CancelFunc
	stalled atomic.Bool
}

func newStallBody(body io.ReadCloser, timeout time.Duration, cancel context.CancelFunc) *stallBody {
	b := &stallBody{body: body, timeout: timeout, cancel: cancel}
	b.timer = time.AfterFunc(timeout, func() {
		b.stalled.Store(true)
		cancel()
	})
	b.timer.Stop()
	return b
}

func (b *stallBody) Read(p []byte) (int, error) {
	b.timer.Reset(b.timeout)
	n, err := b.body.Read(p)
	b.timer.Stop()
	if err != nil && !errors.Is(err, io.EOF) && b.stalled.Load() {
		err = fmt.Errorf("%w for %s", errStalled, b.timeout)
	}
	return n, err
}

func (b *stallBody) Close() error {
	b.timer.Stop()
	err := b.body.Close()
	b.cancel()
	return err
}

// requester sends authenticated GET requests, retrying once with a fresh
// credential when the first one is rejected.
type requester struct {
	// client sends the requests; nil means defaultClient, with a watchdog
	// on every response body.
	client    *http.Client
	token     TokenFunc
	userAgent string
	// headers are set on every request.
	headers map[string]string
	// authHeader is the header the credential goes in. authScheme, when
	// set, precedes the credential, as in "Bearer <token>".
	authHeader string
	authScheme string
	// origin is the API root. A request to another origin carries no
	// credential and does not ask for one.
	origin string
	// stallTimeout is how long a response body may deliver nothing when
	// client is nil; zero means defaultStallTimeout.
	stallTimeout time.Duration
	// askedOnNotFound records that the TokenFunc was already asked again
	// after a 404 without a credential, which happens once per requester.
	askedOnNotFound bool
}

// get sends a GET request and returns the response when its status is 200.
// A 404 becomes an error wrapping notFound.
func (r *requester) get(ctx context.Context, rawURL, accept string, notFound error) (*http.Response, error) {
	authed := r.token != nil && r.authHeader != "" && r.withinOrigin(rawURL)
	var token string
	if authed {
		var err error
		if token, err = r.credential(ctx, false); err != nil {
			return nil, err
		}
	}
	resp, err := r.send(ctx, rawURL, accept, token)
	if err != nil {
		return nil, err
	}
	if authed && !rateLimited(resp) {
		fresh, again, err := r.freshCredential(ctx, resp.StatusCode, token)
		if err != nil {
			drain(resp)
			return nil, err
		}
		if again {
			drain(resp)
			token = fresh
			if resp, err = r.send(ctx, rawURL, accept, token); err != nil {
				return nil, err
			}
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
	// The body, the status text and, for an asset, the URL are the
	// server's: none of it may carry control characters into a terminal.
	detail = strings.TrimSpace(plain(detail))
	statusErr := fmt.Errorf("GET %s: %s", plain(rawURL), plain(resp.Status))
	if detail != "" {
		statusErr = fmt.Errorf("%w: %s", statusErr, detail)
	}
	switch {
	case rateLimited(resp):
		if after := plain(resp.Header.Get("Retry-After")); after != "" {
			return nil, fmt.Errorf("%w: %w (Retry-After: %s)", ErrRateLimited, statusErr, after)
		}
		return nil, fmt.Errorf("%w: %w", ErrRateLimited, statusErr)
	case resp.StatusCode == http.StatusNotFound:
		return nil, fmt.Errorf("%w: %w (a private project answers 404 to a missing or insufficient credential: "+
			"authentication may be needed)", notFound, statusErr)
	case resp.StatusCode == http.StatusUnauthorized, resp.StatusCode == http.StatusForbidden:
		return nil, fmt.Errorf("%w: %w", ErrUnauthorized, statusErr)
	default:
		return nil, fmt.Errorf("selfupdate: %w", statusErr)
	}
}

// freshCredential asks the TokenFunc again after a response with the given
// status to a request that carried token. again reports whether the
// request is to be sent once more, with the credential returned.
func (r *requester) freshCredential(ctx context.Context, status int, token string) (fresh string, again bool, err error) {
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		fresh, err = r.credential(ctx, true)
		return fresh, err == nil, err
	case http.StatusNotFound:
		// A private project answers an anonymous request with 404. Whether
		// that is the case here is unknown, so a TokenFunc that has no
		// credential to offer leaves the 404 as it is.
		if token != "" || r.askedOnNotFound {
			return "", false, nil
		}
		r.askedOnNotFound = true
		if fresh, err = r.credential(ctx, true); err != nil {
			fresh = "" // no credential to try: the 404 is what is reported
		}
		return fresh, fresh != "", nil
	}
	return "", false, nil
}

// withinOrigin reports whether rawURL has the origin of the API root, the
// only origin a credential is sent to.
func (r *requester) withinOrigin(rawURL string) bool {
	if r.origin == "" {
		return true
	}
	target, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	origin, err := url.Parse(r.origin)
	if err != nil {
		return false
	}
	return sameOrigin(target, origin)
}

// rateLimited reports whether resp refuses the request because of a rate
// limit: any 429, and a 403 with a Retry-After header or with a rate-limit
// header (GitHub's X-RateLimit-Remaining, GitLab's RateLimit-Remaining)
// that says no requests remain.
func rateLimited(resp *http.Response) bool {
	switch resp.StatusCode {
	case http.StatusTooManyRequests:
		return true
	case http.StatusForbidden:
		h := resp.Header
		return h.Get("Retry-After") != "" ||
			strings.TrimSpace(h.Get("X-RateLimit-Remaining")) == "0" ||
			strings.TrimSpace(h.Get("RateLimit-Remaining")) == "0"
	}
	return false
}

func (r *requester) credential(ctx context.Context, retry bool) (string, error) {
	if r.token == nil {
		return "", nil
	}
	token, err := r.token(ctx, retry)
	if err != nil {
		return "", fmt.Errorf("selfupdate: credential: %w", err)
	}
	return strings.TrimSpace(token), nil
}

func (r *requester) send(ctx context.Context, rawURL, accept, token string) (*http.Response, error) {
	if r.client != nil {
		return r.do(ctx, r.client, rawURL, accept, token)
	}
	// Without a client of the caller's, nothing but this package bounds the
	// request: the default client's timeouts until the response headers,
	// and a watchdog on the body after them.
	ctx, cancel := context.WithCancel(ctx)
	resp, err := r.do(ctx, defaultClient, rawURL, accept, token)
	if err != nil {
		cancel()
		return nil, err
	}
	timeout := r.stallTimeout
	if timeout <= 0 {
		timeout = defaultStallTimeout
	}
	resp.Body = newStallBody(resp.Body, timeout, cancel)
	return resp, nil
}

func (r *requester) do(ctx context.Context, client *http.Client, rawURL, accept, token string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
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
	if token != "" && r.authHeader != "" {
		if !confidential(req.URL) {
			return nil, fmt.Errorf("selfupdate: refusing to send the credential over plain http to %s: use an https URL",
				plain(req.URL.Host))
		}
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

// confidential reports whether a credential may be sent to u: over https,
// or over http to a loopback address, where nobody else can read it.
func confidential(u *url.URL) bool {
	switch strings.ToLower(u.Scheme) {
	case "https":
		return true
	case "http":
		host := u.Hostname()
		if strings.EqualFold(host, "localhost") {
			return true
		}
		ip := net.ParseIP(host)
		return ip != nil && ip.IsLoopback()
	}
	return false
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

// plain makes text that a server or an archive chose safe to put in an
// error message, which usually ends up on a terminal: line breaks and tabs
// become spaces, and every other control character (C0, DEL, C1), invalid
// UTF-8 and the bidirectional overrides and isolates are removed, so the
// text can neither send an escape sequence nor reorder what is shown.
func plain(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\n', r == '\r', r == '\t', r == '\u2028', r == '\u2029':
			return ' '
		case unicode.IsControl(r):
			return -1
		case r == '\u200e', r == '\u200f', r >= '\u202a' && r <= '\u202e', r >= '\u2066' && r <= '\u2069':
			return -1
		}
		return r
	}, strings.ToValidUTF8(s, ""))
}
