package strava_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/shotah/go-strava-mcp/internal/strava"
)

func TestParseStravaURL(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		wantTyp strava.ResourceType
		wantID  int64
		wantURL string
		ok      bool
	}{
		{
			name:    "activity",
			raw:     "https://www.strava.com/activities/123456",
			wantTyp: strava.ResourceActivity,
			wantID:  123456,
			wantURL: "https://www.strava.com/activities/123456",
			ok:      true,
		},
		{
			name:    "activity overview and query",
			raw:     "https://www.strava.com/activities/99/overview?utm_source=share",
			wantTyp: strava.ResourceActivity,
			wantID:  99,
			wantURL: "https://www.strava.com/activities/99",
			ok:      true,
		},
		{
			name:    "route without www",
			raw:     "https://strava.com/routes/3141592653589793",
			wantTyp: strava.ResourceRoute,
			wantID:  3141592653589793,
			wantURL: "https://www.strava.com/routes/3141592653589793",
			ok:      true,
		},
		{
			name:    "segment",
			raw:     "https://app.strava.com/segments/42",
			wantTyp: strava.ResourceSegment,
			wantID:  42,
			wantURL: "https://www.strava.com/segments/42",
			ok:      true,
		},
		{
			name:    "athlete",
			raw:     "https://m.strava.com/athletes/7",
			wantTyp: strava.ResourceAthlete,
			wantID:  7,
			wantURL: "https://www.strava.com/athletes/7",
			ok:      true,
		},
		{
			name:    "club",
			raw:     "https://www.strava.com/clubs/55/activities",
			wantTyp: strava.ResourceClub,
			wantID:  55,
			wantURL: "https://www.strava.com/clubs/55",
			ok:      true,
		},
		{name: "short link", raw: "https://strava.app.link/abc123", ok: false},
		{name: "missing id", raw: "https://www.strava.com/routes/", ok: false},
		{name: "non-numeric id", raw: "https://www.strava.com/routes/abc", ok: false},
		{name: "unknown path", raw: "https://www.strava.com/dashboard", ok: false},
		{name: "other host", raw: "https://example.com/routes/1", ok: false},
		{name: "empty", raw: "", ok: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := strava.ParseStravaURL(tc.raw)
			if !tc.ok {
				if err == nil {
					t.Fatalf("ParseStravaURL(%q) = %+v, want error", tc.raw, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseStravaURL(%q) error: %v", tc.raw, err)
			}
			if got.Type != tc.wantTyp || got.ID != tc.wantID {
				t.Errorf("got type=%s id=%d, want type=%s id=%d", got.Type, got.ID, tc.wantTyp, tc.wantID)
			}
			if got.CanonicalURL != tc.wantURL {
				t.Errorf("CanonicalURL = %q, want %q", got.CanonicalURL, tc.wantURL)
			}
		})
	}
}

func TestResolvedURLAPIPath(t *testing.T) {
	tests := []struct {
		res     strava.ResolvedURL
		want    string
		wantErr bool
	}{
		{res: strava.ResolvedURL{Type: strava.ResourceActivity, ID: 1}, want: "/activities/1"},
		{res: strava.ResolvedURL{Type: strava.ResourceRoute, ID: 2}, want: "/routes/2"},
		{res: strava.ResolvedURL{Type: strava.ResourceSegment, ID: 3}, want: "/segments/3"},
		{res: strava.ResolvedURL{Type: strava.ResourceAthlete, ID: 4}, want: "/athletes/4"},
		{res: strava.ResolvedURL{Type: strava.ResourceClub, ID: 5}, want: "/clubs/5"},
		{res: strava.ResolvedURL{Type: strava.ResourceType("other"), ID: 6}, wantErr: true},
		{res: strava.ResolvedURL{Type: strava.ResourceRoute, ID: 0}, wantErr: true},
	}
	if _, err := (*strava.ResolvedURL)(nil).APIPath(); err == nil {
		t.Error("nil ResolvedURL.APIPath() = nil, want error")
	}
	for _, tc := range tests {
		got, err := tc.res.APIPath()
		if tc.wantErr {
			if err == nil {
				t.Errorf("APIPath(%+v) = %q, want error", tc.res, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("APIPath(%+v) error: %v", tc.res, err)
			continue
		}
		if got != tc.want {
			t.Errorf("APIPath(%+v) = %q, want %q", tc.res, got, tc.want)
		}
	}
}

func newTestResolver(t *testing.T, srv *httptest.Server) *strava.URLResolver {
	t.Helper()
	r := strava.NewURLResolver()
	r.AllowHTTP()
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	r.AllowHost(u.Hostname())
	return r
}

func TestResolveCanonicalURLNoHTTP(t *testing.T) {
	r := strava.NewURLResolver()
	got, err := r.Resolve(context.Background(), "https://www.strava.com/routes/88")
	if err != nil {
		t.Fatalf("Resolve() error: %v", err)
	}
	if got.Type != strava.ResourceRoute || got.ID != 88 {
		t.Errorf("got type=%s id=%d, want route 88", got.Type, got.ID)
	}
	if got.InputURL != "https://www.strava.com/routes/88" {
		t.Errorf("InputURL = %q", got.InputURL)
	}
}

func TestResolveAddsHTTPSAndTrims(t *testing.T) {
	r := strava.NewURLResolver()
	got, err := r.Resolve(context.Background(), "  www.strava.com/activities/12  ")
	if err != nil {
		t.Fatalf("Resolve() error: %v", err)
	}
	if got.ID != 12 || got.Type != strava.ResourceActivity {
		t.Errorf("got type=%s id=%d, want activity 12", got.Type, got.ID)
	}
}

func TestResolveFollowsShortLinkRedirect(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/abc123" {
			t.Errorf("path = %q, want /abc123", r.URL.Path)
		}
		if r.Header.Get("User-Agent") == "" {
			t.Error("expected User-Agent")
		}
		http.Redirect(w, r, "https://www.strava.com/routes/777", http.StatusFound)
	}))
	defer srv.Close()

	resolver := newTestResolver(t, srv)
	got, err := resolver.Resolve(context.Background(), srv.URL+"/abc123")
	if err != nil {
		t.Fatalf("Resolve() error: %v", err)
	}
	if got.Type != strava.ResourceRoute || got.ID != 777 {
		t.Errorf("got type=%s id=%d, want route 777", got.Type, got.ID)
	}
	if got.CanonicalURL != "https://www.strava.com/routes/777" {
		t.Errorf("CanonicalURL = %q", got.CanonicalURL)
	}
}

func TestResolveFollowsRelativeRedirectThenHTML(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/short":
			http.Redirect(w, r, "/interstitial", http.StatusMovedPermanently)
		case "/interstitial":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`<html><head><meta property="og:url" content="https://www.strava.com/activities/4242"></head></html>`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	resolver := newTestResolver(t, srv)
	got, err := resolver.Resolve(context.Background(), srv.URL+"/short")
	if err != nil {
		t.Fatalf("Resolve() error: %v", err)
	}
	if got.Type != strava.ResourceActivity || got.ID != 4242 {
		t.Errorf("got type=%s id=%d, want activity 4242", got.Type, got.ID)
	}
}

func TestResolveRejectsNonHTTPS(t *testing.T) {
	r := strava.NewURLResolver()
	_, err := r.Resolve(context.Background(), "http://www.strava.com/routes/1")
	if err == nil || !strings.Contains(err.Error(), "non-https") {
		t.Fatalf("Resolve() error = %v, want non-https", err)
	}
}

func TestResolveRejectsForeignHost(t *testing.T) {
	r := strava.NewURLResolver()
	_, err := r.Resolve(context.Background(), "https://evil.example/routes/1")
	if err == nil || !strings.Contains(err.Error(), "non-Strava host") {
		t.Fatalf("Resolve() error = %v, want non-Strava host", err)
	}
}

func TestResolveRejectsRedirectToForeignHost(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://evil.example/steal", http.StatusFound)
	}))
	defer srv.Close()

	resolver := newTestResolver(t, srv)
	_, err := resolver.Resolve(context.Background(), srv.URL+"/short")
	if err == nil || !strings.Contains(err.Error(), "non-Strava host") {
		t.Fatalf("Resolve() error = %v, want non-Strava host", err)
	}
}

func TestResolveEmptyURL(t *testing.T) {
	r := strava.NewURLResolver()
	_, err := r.Resolve(context.Background(), "   ")
	if err == nil || !strings.Contains(err.Error(), "url is required") {
		t.Fatalf("Resolve() error = %v, want url is required", err)
	}
}

func TestResolveUnsupportedScheme(t *testing.T) {
	r := strava.NewURLResolver()
	_, err := r.Resolve(context.Background(), "javascript:alert(1)")
	if err == nil || !strings.Contains(err.Error(), "unsupported url scheme") {
		t.Fatalf("Resolve() error = %v, want unsupported scheme", err)
	}
}

func TestResolveUnresolvableHTML(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`<html><body>no strava links here</body></html>`))
	}))
	defer srv.Close()

	resolver := newTestResolver(t, srv)
	_, err := resolver.Resolve(context.Background(), srv.URL+"/short")
	if err == nil || !strings.Contains(err.Error(), "could not resolve") {
		t.Fatalf("Resolve() error = %v, want could not resolve", err)
	}
}

func TestResolveTooManyRedirects(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/loop", http.StatusFound)
	}))
	defer srv.Close()

	resolver := newTestResolver(t, srv)
	_, err := resolver.Resolve(context.Background(), srv.URL+"/loop")
	if err == nil || !strings.Contains(err.Error(), "too many redirects") {
		t.Fatalf("Resolve() error = %v, want too many redirects", err)
	}
}

func TestResolveInvalidURL(t *testing.T) {
	r := strava.NewURLResolver()
	_, err := r.Resolve(context.Background(), "https://www.strava.com/%zz")
	if err == nil {
		t.Fatal("Resolve() = nil, want parse error")
	}
}

func TestParseStravaURLInvalidSyntax(t *testing.T) {
	_, err := strava.ParseStravaURL("https://www.strava.com/%zz")
	if err == nil {
		t.Fatal("ParseStravaURL() = nil, want parse error")
	}
}

func TestResolveInvalidRedirectLocation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "://not-a-url")
		w.WriteHeader(http.StatusFound)
	}))
	defer srv.Close()

	resolver := newTestResolver(t, srv)
	_, err := resolver.Resolve(context.Background(), srv.URL+"/short")
	if err == nil {
		t.Fatal("Resolve() = nil, want an error for a broken Location header")
	}
}

func TestResolveRequestError(t *testing.T) {
	r := strava.NewURLResolver()
	r.AllowHTTP()
	r.AllowHost("127.0.0.1")
	_, err := r.Resolve(context.Background(), "http://127.0.0.1:1/short")
	if err == nil || !strings.Contains(err.Error(), "resolve request") {
		t.Fatalf("Resolve() error = %v, want resolve request", err)
	}
}
