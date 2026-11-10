package selfupdate

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
		case "/api/v4/projects/example%2Ftool/releases/permalink/latest":
			// GitLab answers the permalink with a redirect to the release.
			http.Redirect(w, r, "/api/v4/projects/example%2Ftool/releases/v1.2.3", http.StatusFound)
		case "/api/v4/projects/example%2Ftool/releases/v1.2.3":
			_, _ = io.WriteString(w, `{"tag_name":"v1.2.3"}`)
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
			_, _ = io.WriteString(w, `{"tag_name":"v1.0.0"}`)
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
		_, _ = io.WriteString(w, `{"tag_name":"v1.0.0"}`)
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
