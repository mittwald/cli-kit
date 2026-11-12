package selfupdate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode"
)

func readAll(t *testing.T, rc io.ReadCloser) string {
	t.Helper()
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestGitLabLatestAndDownload(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.EscapedPath())
		if r.Header.Get("PRIVATE-TOKEN") != "tok" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		switch r.URL.EscapedPath() {
		case "/api/v4/projects/example%2Ftool/releases":
			_, _ = io.WriteString(w, `[{"tag_name":"v1.2.3"},{"tag_name":"v1.2.2"}]`)
		case "/api/v4/projects/example%2Ftool/packages/generic/tool/1.2.3/tool_Linux_x86_64.tar.gz":
			_, _ = io.WriteString(w, "archive")
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	g := &GitLab{BaseURL: srv.URL, Project: "example/tool", Package: "tool",
		Token: func(context.Context, bool) (string, error) { return "tok", nil }}
	rel, err := g.Latest(context.Background())
	if err != nil || rel.Tag != "v1.2.3" {
		t.Fatalf("Latest = %+v, %v", rel, err)
	}
	body, err := g.Download(context.Background(), rel, "tool_Linux_x86_64.tar.gz")
	if err != nil {
		t.Fatalf("Download = %v", err)
	}
	if got := readAll(t, body); got != "archive" {
		t.Fatalf("Download body = %q", got)
	}

	_, err = g.Download(context.Background(), rel, "missing.zip")
	if !errors.Is(err, ErrAssetNotFound) {
		t.Fatalf("Download missing = %v, want ErrAssetNotFound", err)
	}
	// Both the stripped version and the raw tag are tried.
	if !strings.Contains(strings.Join(paths, " "), "/tool/v1.2.3/missing.zip") {
		t.Fatalf("raw tag not tried; requests: %v", paths)
	}
}

func TestGitLabTokenHeaders(t *testing.T) {
	for header, check := range map[string]func(*http.Request) bool{
		HeaderJobToken:      func(r *http.Request) bool { return r.Header.Get("JOB-TOKEN") == "tok" },
		HeaderDeployToken:   func(r *http.Request) bool { return r.Header.Get("DEPLOY-TOKEN") == "tok" },
		HeaderAuthorization: func(r *http.Request) bool { return r.Header.Get("Authorization") == "Bearer tok" },
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !check(r) {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			_, _ = io.WriteString(w, `[{"tag_name":"v1.0.0"}]`)
		}))
		g := &GitLab{BaseURL: srv.URL + "/api/v4", Project: "42", TokenHeader: header,
			Token: func(context.Context, bool) (string, error) { return "tok", nil }}
		if _, err := g.Latest(context.Background()); err != nil {
			t.Errorf("%s: Latest = %v", header, err)
		}
		srv.Close()
	}
}

func TestRetriesOnceWithAFreshToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("PRIVATE-TOKEN") != "fresh" {
			// Some servers echo the request; the token must not reach the error.
			http.Error(w, "bad token "+r.Header.Get("PRIVATE-TOKEN"), http.StatusUnauthorized)
			return
		}
		_, _ = io.WriteString(w, `[{"tag_name":"v1.0.0"}]`)
	}))
	defer srv.Close()

	var retries []bool
	g := &GitLab{BaseURL: srv.URL, Project: "example/tool", Token: func(_ context.Context, retry bool) (string, error) {
		retries = append(retries, retry)
		if retry {
			return "fresh", nil
		}
		return "stale", nil
	}}
	if _, err := g.Latest(context.Background()); err != nil {
		t.Fatalf("Latest = %v", err)
	}
	if len(retries) != 2 || retries[0] || !retries[1] {
		t.Fatalf("token calls = %v, want [false true]", retries)
	}

	g.Token = func(context.Context, bool) (string, error) { return "secret-value", nil }
	_, err := g.Latest(context.Background())
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("Latest with a bad token = %v, want ErrUnauthorized", err)
	}
	if strings.Contains(err.Error(), "secret-value") {
		t.Fatalf("error carries the token: %v", err)
	}

	fixed := errors.New("token comes from $EXAMPLE_TOKEN")
	g.Token = func(_ context.Context, retry bool) (string, error) {
		if retry {
			return "", fixed
		}
		return "stale", nil
	}
	if _, err := g.Latest(context.Background()); !errors.Is(err, fixed) {
		t.Fatalf("Latest = %v, want the TokenFunc's refusal", err)
	}
}

func TestGitLabNoRelease(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	g := &GitLab{BaseURL: srv.URL, Project: "example/tool"}
	if _, err := g.Latest(context.Background()); !errors.Is(err, ErrNoRelease) {
		t.Fatalf("Latest = %v, want ErrNoRelease", err)
	}
	if _, err := (&GitLab{}).Latest(context.Background()); err == nil {
		t.Fatal("Latest without BaseURL succeeded")
	}
	empty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `[]`)
	}))
	defer empty.Close()
	g.BaseURL = empty.URL
	if _, err := g.Latest(context.Background()); !errors.Is(err, ErrNoRelease) {
		t.Fatalf("Latest of a project without releases = %v, want ErrNoRelease", err)
	}
}

// gitLabReleases serves pages of releases with the given tags, newest
// first, and counts the requests.
func gitLabReleases(t *testing.T, tags []string) (*GitLab, *int) {
	t.Helper()
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		q := r.URL.Query()
		page, _ := strconv.Atoi(q.Get("page"))
		perPage, _ := strconv.Atoi(q.Get("per_page"))
		if page < 1 || perPage < 1 || q.Get("order_by") != "released_at" || q.Get("sort") != "desc" {
			http.Error(w, "unexpected query "+r.URL.RawQuery, http.StatusBadRequest)
			return
		}
		var body []map[string]string
		for i := (page - 1) * perPage; i < min(page*perPage, len(tags)); i++ {
			body = append(body, map[string]string{"tag_name": tags[i]})
		}
		if body == nil {
			body = []map[string]string{}
		}
		_ = json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(srv.Close)
	return &GitLab{BaseURL: srv.URL, Project: "example/tool"}, &requests
}

// GitLab marks no release as a pre-release, so the tag has to tell: the
// latest release is the highest stable version, not the newest release.
func TestGitLabLatestIsTheHighestStableRelease(t *testing.T) {
	g, requests := gitLabReleases(t, []string{"v2.0.0-rc.1", "nightly", "v1.9.5", "v1.10.0", "v1.9.4", "v2.0.0-beta.1"})
	rel, err := g.Latest(context.Background())
	if err != nil || rel.Tag != "v1.10.0" {
		t.Fatalf("Latest = %+v, %v; want v1.10.0", rel, err)
	}
	if *requests != 1 {
		t.Errorf("Latest sent %d requests, want 1", *requests)
	}

	// A first page of pre-releases only: the next page is read.
	var tags []string
	for i := range gitLabReleasesPerPage {
		tags = append(tags, fmt.Sprintf("v3.0.0-rc.%d", gitLabReleasesPerPage-i))
	}
	g, requests = gitLabReleases(t, append(tags, "v2.1.0", "v2.0.0"))
	if rel, err = g.Latest(context.Background()); err != nil || rel.Tag != "v2.1.0" {
		t.Fatalf("Latest behind a page of pre-releases = %+v, %v; want v2.1.0", rel, err)
	}
	if *requests != 2 {
		t.Errorf("Latest sent %d requests, want 2", *requests)
	}

	// Pre-releases only, however far back: the search ends.
	tags = nil
	for i := range 10 * gitLabReleasesPerPage {
		tags = append(tags, fmt.Sprintf("v3.0.0-rc.%d", 1000-i))
	}
	g, requests = gitLabReleases(t, tags)
	if _, err = g.Latest(context.Background()); !errors.Is(err, ErrNoRelease) {
		t.Fatalf("Latest of pre-releases only = %v, want ErrNoRelease", err)
	}
	if *requests != gitLabReleasePages {
		t.Errorf("Latest sent %d requests, want it to stop after %d", *requests, gitLabReleasePages)
	}

	g, _ = gitLabReleases(t, []string{"v1.0.0-rc.1", "latest"})
	if _, err = g.Latest(context.Background()); !errors.Is(err, ErrNoRelease) || !strings.Contains(err.Error(), "none of the 2") {
		t.Fatalf("Latest without a stable release = %v, want ErrNoRelease naming the releases seen", err)
	}
}

// A rate limit is not the credential's fault: the TokenFunc is not asked
// for another one, which would make it forget a valid token, and the error
// is not ErrUnauthorized.
func TestRateLimitIsNoRejectedCredential(t *testing.T) {
	tests := []struct {
		label   string
		status  int
		headers map[string]string
		limited bool
	}{
		{"GitHub primary limit", http.StatusForbidden, map[string]string{"X-RateLimit-Remaining": "0"}, true},
		{"GitHub secondary limit", http.StatusForbidden, map[string]string{"Retry-After": "60"}, true},
		{"GitLab header", http.StatusForbidden, map[string]string{"RateLimit-Remaining": "0"}, true},
		{"429", http.StatusTooManyRequests, nil, true},
		{"403 with requests remaining", http.StatusForbidden, map[string]string{"X-RateLimit-Remaining": "41"}, false},
		{"plain 403", http.StatusForbidden, nil, false},
	}
	for _, tt := range tests {
		requests := 0
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			requests++
			for name, value := range tt.headers {
				w.Header()[name] = []string{value}
			}
			w.WriteHeader(tt.status)
			_, _ = io.WriteString(w, `{"message":"API rate limit exceeded"}`)
		}))
		var calls []bool
		g := &GitHub{Owner: "example", Repo: "tool", APIURL: srv.URL, Token: func(_ context.Context, retry bool) (string, error) {
			calls = append(calls, retry)
			return "valid-token", nil
		}}
		_, err := g.Latest(context.Background())
		srv.Close()
		if tt.limited {
			if !errors.Is(err, ErrRateLimited) || errors.Is(err, ErrUnauthorized) {
				t.Errorf("%s: Latest = %v, want ErrRateLimited and not ErrUnauthorized", tt.label, err)
			}
			if len(calls) != 1 || calls[0] || requests != 1 {
				t.Errorf("%s: token calls = %v, requests = %d; want one of each and no retry", tt.label, calls, requests)
			}
		} else {
			if !errors.Is(err, ErrUnauthorized) || errors.Is(err, ErrRateLimited) {
				t.Errorf("%s: Latest = %v, want ErrUnauthorized", tt.label, err)
			}
			if len(calls) != 2 || !calls[1] || requests != 2 {
				t.Errorf("%s: token calls = %v, requests = %d; want a retry with a fresh token", tt.label, calls, requests)
			}
		}
		if after := tt.headers["Retry-After"]; after != "" && !strings.Contains(err.Error(), "Retry-After: "+after) {
			t.Errorf("%s: error does not say when to retry: %v", tt.label, err)
		}
	}
}

// A private project answers an anonymous request with 404. A TokenFunc
// that offered no credential is asked once more, and the error says that
// authentication may be what is missing.
func TestNotFoundAsksOnceForACredential(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Header.Get("PRIVATE-TOKEN") != "tok":
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"message":"404 Project Not Found"}`)
		case strings.HasSuffix(r.URL.Path, "/releases"):
			_, _ = io.WriteString(w, `[{"tag_name":"v1.0.0"}]`)
		case strings.HasSuffix(r.URL.Path, "/1.0.0/tool.tar.gz"):
			_, _ = io.WriteString(w, "archive")
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	var calls []bool
	stored := ""
	g := &GitLab{BaseURL: srv.URL, Project: "example/tool", Package: "tool",
		Token: func(_ context.Context, retry bool) (string, error) {
			calls = append(calls, retry)
			if retry {
				stored = "tok" // the user was asked
			}
			return stored, nil
		}}
	rel, err := g.Latest(context.Background())
	if err != nil || rel.Tag != "v1.0.0" {
		t.Fatalf("Latest = %+v, %v", rel, err)
	}
	if len(calls) != 2 || calls[0] || !calls[1] {
		t.Fatalf("token calls = %v, want [false true]", calls)
	}

	// No credential to offer: the 404 stands, with a hint, and both package
	// versions of a download ask only once between them.
	calls = nil
	g.Token = func(_ context.Context, retry bool) (string, error) {
		calls = append(calls, retry)
		return "", nil
	}
	_, err = g.Download(context.Background(), rel, "tool.tar.gz")
	if !errors.Is(err, ErrAssetNotFound) || !strings.Contains(err.Error(), "authentication may be needed") {
		t.Fatalf("Download = %v, want ErrAssetNotFound with a hint at authentication", err)
	}
	if want := []bool{false, true, false}; !slices.Equal(calls, want) {
		t.Fatalf("token calls = %v, want %v", calls, want)
	}
	if _, err = g.Latest(context.Background()); !errors.Is(err, ErrNoRelease) || !strings.Contains(err.Error(), "authentication may be needed") {
		t.Fatalf("Latest = %v, want ErrNoRelease with a hint at authentication", err)
	}

	// A TokenFunc that fails when asked again leaves the 404 standing too.
	g.Token = func(_ context.Context, retry bool) (string, error) {
		if retry {
			return "", errors.New("no terminal to ask on")
		}
		return "", nil
	}
	if _, err = g.Latest(context.Background()); !errors.Is(err, ErrNoRelease) {
		t.Fatalf("Latest = %v, want ErrNoRelease", err)
	}

	// A credential that was sent is not asked for again after a 404.
	calls = nil
	g.Token = func(_ context.Context, retry bool) (string, error) {
		calls = append(calls, retry)
		return "tok", nil
	}
	if _, err = g.Download(context.Background(), rel, "missing.zip"); !errors.Is(err, ErrAssetNotFound) {
		t.Fatalf("Download = %v, want ErrAssetNotFound", err)
	}
	if slices.Contains(calls, true) {
		t.Fatalf("token calls = %v, want no retry", calls)
	}
}

// What the server sends back reaches the error as plain text.
func TestErrorsCarryNoControlCharacters(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, "\x1b]52;c;ZXZpbA==\x07\x1b[2K\rall\tgood\u009b31m\u202e\xff")
	}))
	defer srv.Close()
	_, err := (&GitHub{Owner: "example", Repo: "tool", APIURL: srv.URL}).Latest(context.Background())
	if err == nil {
		t.Fatal("Latest succeeded")
	}
	if strings.ContainsFunc(err.Error(), func(r rune) bool { return unicode.IsControl(r) || r == '\u202e' || r == unicode.ReplacementChar }) {
		t.Fatalf("error carries control characters: %q", err.Error())
	}
	if !strings.HasSuffix(err.Error(), "500 Internal Server Error: ]52;c;ZXZpbA==[2K all good31m") {
		t.Fatalf("error lost the server's text: %q", err.Error())
	}
	for in, want := range map[string]string{
		"plain text, as it is":   "plain text, as it is",
		"two\nlines\r\n":         "two lines  ",
		"\x00\x7f\u0085ü\u2066é": "üé",
	} {
		if got := plain(in); got != want {
			t.Errorf("plain(%q) = %q, want %q", in, got, want)
		}
	}
}

// The credential belongs to the API. An asset URL that points elsewhere is
// fetched without it, and nothing asks for a credential it will not send.
func TestCredentialOnlyGoesToTheAPIOrigin(t *testing.T) {
	var elsewhereSaw []string
	elsewhere := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		elsewhereSaw = append(elsewhereSaw, r.Header.Get("Authorization"))
		if r.URL.Path == "/private" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = io.WriteString(w, "archive")
	}))
	defer elsewhere.Close()
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = io.WriteString(w, "from the API")
	}))
	defer api.Close()

	calls := 0
	g := &GitHub{Owner: "example", Repo: "tool", APIURL: api.URL, Token: func(context.Context, bool) (string, error) {
		calls++
		return "tok", nil
	}}
	rel := Release{Tag: "v1.0.0", Assets: []Asset{
		{Name: "elsewhere.tar.gz", URL: elsewhere.URL + "/blob"},
		{Name: "private.tar.gz", URL: elsewhere.URL + "/private"},
		{Name: "api.tar.gz", URL: api.URL + "/repos/example/tool/releases/assets/1"},
	}}
	body, err := g.Download(context.Background(), rel, "elsewhere.tar.gz")
	if err != nil {
		t.Fatalf("Download = %v", err)
	}
	if got := readAll(t, body); got != "archive" {
		t.Fatalf("Download body = %q", got)
	}
	if _, err = g.Download(context.Background(), rel, "private.tar.gz"); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("Download from another origin that wants a credential = %v, want ErrUnauthorized", err)
	}
	if calls != 0 {
		t.Errorf("the TokenFunc was asked %d times for requests that carry no credential", calls)
	}
	for _, got := range elsewhereSaw {
		if got != "" {
			t.Errorf("another origin received Authorization %q", got)
		}
	}
	if body, err = g.Download(context.Background(), rel, "api.tar.gz"); err != nil {
		t.Fatalf("Download from the API origin = %v", err)
	}
	if got := readAll(t, body); got != "from the API" {
		t.Fatalf("Download body = %q", got)
	}
}

// roundTripFunc is an http.RoundTripper made of a function.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// A credential is not sent in the clear: over plain http the request is
// refused before it leaves, unless it goes to the machine itself.
func TestCredentialNeverGoesOverPlainHTTP(t *testing.T) {
	sent := 0
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		sent++
		if r.Header.Get("Authorization") != "" || r.Header.Get("PRIVATE-TOKEN") != "" {
			t.Errorf("request to %s carries the credential", r.URL)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`[]`)), Header: http.Header{}}, nil
	})}
	token := func(context.Context, bool) (string, error) { return "secret-token", nil }

	_, err := (&GitHub{Owner: "example", Repo: "tool", APIURL: "http://github.example.com/api/v3", Token: token, Client: client}).
		Latest(context.Background())
	if err == nil || !strings.Contains(err.Error(), "plain http") || strings.Contains(err.Error(), "secret-token") {
		t.Fatalf("GitHub.Latest over http = %v, want the request refused", err)
	}
	_, err = (&GitLab{BaseURL: "http://gitlab.example.com", Project: "example/tool", Token: token, Client: client}).
		Latest(context.Background())
	if err == nil || !strings.Contains(err.Error(), "plain http") {
		t.Fatalf("GitLab.Latest over http = %v, want the request refused", err)
	}
	if sent != 0 {
		t.Fatalf("%d requests were sent", sent)
	}
	// Without a credential there is nothing to protect.
	if _, err = (&GitLab{BaseURL: "http://gitlab.example.com", Project: "example/tool", Client: client}).
		Latest(context.Background()); !errors.Is(err, ErrNoRelease) || sent != 1 {
		t.Fatalf("anonymous Latest over http = %v after %d requests, want it sent", err, sent)
	}

	for rawURL, want := range map[string]bool{
		"https://example.com/x":    true,
		"http://example.com/x":     false,
		"http://localhost:8080/x":  true,
		"http://127.0.0.1:8080/x":  true,
		"http://127.8.9.10/x":      true,
		"http://[::1]:8080/x":      true,
		"http://10.0.0.1/x":        false,
		"http://localhost.evil./x": false,
		"ftp://localhost/x":        false,
	} {
		u, err := url.Parse(rawURL)
		if err != nil {
			t.Fatal(err)
		}
		if got := confidential(u); got != want {
			t.Errorf("confidential(%s) = %v, want %v", rawURL, got, want)
		}
	}
}

// A source without a client of its own does not wait forever: not for the
// response headers, and not for a body that stops arriving.
func TestDefaultClientGivesUpOnAStalledServer(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/body" {
			w.Header().Set("Content-Length", "1000")
			_, _ = io.WriteString(w, "a start")
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(release)

	if transport, ok := defaultClient.Transport.(*http.Transport); !ok || transport.ResponseHeaderTimeout <= 0 ||
		transport.TLSHandshakeTimeout <= 0 || defaultClient.Timeout != 0 {
		t.Fatalf("default client has no header and handshake timeouts, or limits the whole request: %+v", defaultClient)
	}
	start := time.Now()
	resp, err := newClient(50 * time.Millisecond).Get(srv.URL + "/headers")
	if err == nil {
		_ = resp.Body.Close()
		t.Fatal("a server that sends no response headers was waited for")
	}
	if waited := time.Since(start); waited > 10*time.Second {
		t.Fatalf("gave up on the headers after %s", waited)
	}

	r := &requester{stallTimeout: 50 * time.Millisecond}
	resp, err = r.get(context.Background(), srv.URL+"/body", "", ErrAssetNotFound)
	if err != nil {
		t.Fatalf("get = %v", err)
	}
	defer resp.Body.Close()
	start = time.Now()
	data, err := io.ReadAll(resp.Body)
	if !errors.Is(err, errStalled) || string(data) != "a start" {
		t.Fatalf("read %q, %v; want the stalled body given up", data, err)
	}
	if waited := time.Since(start); waited > 10*time.Second {
		t.Fatalf("gave up on the body after %s", waited)
	}

	// A caller's client is used as it is: no watchdog on its bodies.
	theirs := &requester{client: srv.Client(), stallTimeout: 50 * time.Millisecond}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	resp, err = theirs.get(ctx, srv.URL+"/body", "", ErrAssetNotFound)
	if err != nil {
		t.Fatalf("get = %v", err)
	}
	defer resp.Body.Close()
	if _, err = io.ReadAll(resp.Body); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("read with the caller's client = %v, want only the context's deadline to end it", err)
	}
	if (&GitHub{}).requester().client != nil || (&GitLab{}).requester().client != nil {
		t.Fatal("a source without a Client does not use the default client")
	}
}

// A body that keeps arriving is never given up, however long it takes in
// all and however slowly it is read.
func TestStallTimeoutCountsOnlyWaiting(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		for range 6 {
			_, _ = io.WriteString(w, "chunk ")
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			time.Sleep(40 * time.Millisecond)
		}
	}))
	defer srv.Close()
	r := &requester{stallTimeout: 150 * time.Millisecond}
	resp, err := r.get(context.Background(), srv.URL, "", ErrAssetNotFound)
	if err != nil {
		t.Fatalf("get = %v", err)
	}
	defer resp.Body.Close()
	time.Sleep(200 * time.Millisecond) // a slow reader is not a stalled server
	data, err := io.ReadAll(resp.Body)
	if err != nil || string(data) != strings.Repeat("chunk ", 6) {
		t.Fatalf("read %q, %v", data, err)
	}
}

func TestGitHubLatestAndDownload(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") == "" || r.Header.Get("X-GitHub-Api-Version") == "" {
			http.Error(w, "missing headers", http.StatusBadRequest)
			return
		}
		if r.Header.Get("Authorization") != "Bearer tok" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/repos/example/tool/releases/latest":
			_, _ = io.WriteString(w, `{"tag_name":"v2.0.0","assets":[{"name":"tool_Linux_x86_64.tar.gz","url":"`+
				srv.URL+`/repos/example/tool/releases/assets/7"}]}`)
		case "/repos/example/tool/releases/assets/7":
			if r.Header.Get("Accept") != "application/octet-stream" {
				http.Error(w, "wrong accept", http.StatusBadRequest)
				return
			}
			_, _ = io.WriteString(w, "archive")
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	g := &GitHub{Owner: "example", Repo: "tool", APIURL: srv.URL,
		Token: func(context.Context, bool) (string, error) { return "tok", nil }}
	rel, err := g.Latest(context.Background())
	if err != nil || rel.Tag != "v2.0.0" || len(rel.Assets) != 1 {
		t.Fatalf("Latest = %+v, %v", rel, err)
	}
	body, err := g.Download(context.Background(), rel, "tool_Linux_x86_64.tar.gz")
	if err != nil {
		t.Fatalf("Download = %v", err)
	}
	if got := readAll(t, body); got != "archive" {
		t.Fatalf("Download body = %q", got)
	}
	if _, err := g.Download(context.Background(), rel, "checksums.txt"); !errors.Is(err, ErrAssetNotFound) {
		t.Fatalf("Download missing = %v, want ErrAssetNotFound", err)
	}
	if _, err := (&GitHub{}).Latest(context.Background()); err == nil {
		t.Fatal("Latest without Owner succeeded")
	}
}

func TestCredentialNotSentToOtherOrigins(t *testing.T) {
	for _, header := range []string{HeaderPrivateToken, HeaderJobToken, HeaderDeployToken, HeaderAuthorization} {
		t.Run(header, func(t *testing.T) {
			var storageSaw []string
			var gitlab *httptest.Server
			// storage serves the file after the first redirect and bounces
			// a second request back to the GitLab origin.
			storage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				storageSaw = append(storageSaw, r.Header.Get(header))
				if r.URL.Path == "/bounce" {
					http.Redirect(w, r, gitlab.URL+"/back", http.StatusFound)
					return
				}
				_, _ = io.WriteString(w, "archive")
			}))
			defer storage.Close()
			var backSaw string
			gitlab = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.URL.Path == "/back":
					backSaw = r.Header.Get(header)
					_, _ = io.WriteString(w, "archive")
				case strings.HasSuffix(r.URL.Path, "/a.tar.gz"):
					http.Redirect(w, r, storage.URL+"/blob", http.StatusFound)
				case strings.HasSuffix(r.URL.Path, "/b.tar.gz"):
					http.Redirect(w, r, storage.URL+"/bounce", http.StatusFound)
				default:
					http.NotFound(w, r)
				}
			}))
			defer gitlab.Close()

			g := &GitLab{BaseURL: gitlab.URL, Project: "example/tool", Package: "tool", TokenHeader: header,
				Token: func(context.Context, bool) (string, error) { return "secret-token", nil }}
			for _, name := range []string{"a.tar.gz", "b.tar.gz"} {
				body, err := g.Download(context.Background(), Release{Tag: "v1.0.0"}, name)
				if err != nil {
					t.Fatalf("Download %s = %v", name, err)
				}
				readAll(t, body)
			}
			for _, got := range storageSaw {
				if got != "" {
					t.Errorf("storage host received %s %q", header, got)
				}
			}
			if backSaw != "" {
				t.Errorf("redirect back to the origin received %s %q after leaving it", header, backSaw)
			}
		})
	}
}

func TestCredentialRedirectPolicyKeepsClientPolicy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/elsewhere", http.StatusFound)
	}))
	defer srv.Close()
	stop := errors.New("no redirects")
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return stop }}
	g := &GitLab{BaseURL: srv.URL, Project: "example/tool", Client: client,
		Token: func(context.Context, bool) (string, error) { return "tok", nil }}
	if _, err := g.Latest(context.Background()); !errors.Is(err, stop) {
		t.Fatalf("Latest = %v, want the client's CheckRedirect error", err)
	}
	if client.CheckRedirect == nil {
		t.Fatal("client's CheckRedirect was replaced")
	}
}
