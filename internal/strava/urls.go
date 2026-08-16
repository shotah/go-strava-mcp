package strava

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	maxRedirects = 8
	maxHTMLBytes = 512 << 10
	resolveUA    = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
)

// ResourceType is a Strava web resource that a share URL can point at.
type ResourceType string

const (
	ResourceActivity ResourceType = "activity"
	ResourceRoute    ResourceType = "route"
	ResourceSegment  ResourceType = "segment"
	ResourceAthlete  ResourceType = "athlete"
	ResourceClub     ResourceType = "club"
)

var pathTypeBySegment = map[string]ResourceType{
	"activities": ResourceActivity,
	"routes":     ResourceRoute,
	"segments":   ResourceSegment,
	"athletes":   ResourceAthlete,
	"clubs":      ResourceClub,
}

// resourceHosts are www/app hosts whose paths encode a resource id.
var resourceHosts = map[string]bool{
	"strava.com":     true,
	"www.strava.com": true,
	"app.strava.com": true,
	"m.strava.com":   true,
}

// redirectHosts may appear in a short-link chain but are not resource pages.
var redirectHosts = map[string]bool{
	"strava.app.link":     true,
	"www.strava.app.link": true,
	"app.link":            true,
	"bnc.lt":              true,
}

// htmlResourceURLRe finds the first Strava resource URL in a Branch/HTML interstitial.
var htmlResourceURLRe = regexp.MustCompile(`https?://(?:www\.|app\.|m\.)?strava\.com/(?:activities|routes|segments|athletes|clubs)/[0-9]+`)

var errNotResourceURL = errors.New("not a Strava resource URL")

// ResolvedURL is a Strava share/short link reduced to a typed resource id.
type ResolvedURL struct {
	InputURL     string
	CanonicalURL string
	Type         ResourceType
	ID           int64
}

// URLResolver follows Strava short links (strava.app.link) to a resource URL.
// Requests are unauthenticated and restricted to known Strava/Branch hosts.
type URLResolver struct {
	httpClient   *http.Client
	allowedHosts map[string]bool
	allowHTTP    bool
}

// NewURLResolver returns a resolver that only talks to Strava and Branch hosts.
func NewURLResolver() *URLResolver {
	r := &URLResolver{
		allowedHosts: defaultAllowedHosts(),
	}
	r.httpClient = &http.Client{
		Timeout: 15 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	return r
}

func defaultAllowedHosts() map[string]bool {
	hosts := make(map[string]bool, len(resourceHosts)+len(redirectHosts))
	for host := range resourceHosts {
		hosts[host] = true
	}
	for host := range redirectHosts {
		hosts[host] = true
	}
	return hosts
}

// AllowHost adds a host to the resolver allowlist. Intended for tests.
func (r *URLResolver) AllowHost(host string) {
	if r.allowedHosts == nil {
		r.allowedHosts = defaultAllowedHosts()
	}
	r.allowedHosts[strings.ToLower(host)] = true
}

// AllowHTTP permits http:// URLs. Intended for httptest servers.
func (r *URLResolver) AllowHTTP() {
	r.allowHTTP = true
}

// ParseStravaURL extracts a resource type and id from a canonical Strava URL.
// Short links (strava.app.link) are not parseable and must be Resolve'd first.
func ParseStravaURL(raw string) (*ResolvedURL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, fmt.Errorf("parse url: %w", err)
	}
	if u.Host == "" || u.Path == "" {
		return nil, errNotResourceURL
	}
	if !resourceHosts[strings.ToLower(u.Hostname())] {
		return nil, errNotResourceURL
	}

	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 2 {
		return nil, errNotResourceURL
	}
	resType, ok := pathTypeBySegment[strings.ToLower(parts[0])]
	if !ok {
		return nil, errNotResourceURL
	}
	id, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || id <= 0 {
		return nil, errNotResourceURL
	}

	return &ResolvedURL{
		InputURL:     raw,
		CanonicalURL: fmt.Sprintf("https://www.strava.com/%s/%d", parts[0], id),
		Type:         resType,
		ID:           id,
	}, nil
}

// Resolve follows a Strava URL or short link to a typed resource.
func (r *URLResolver) Resolve(ctx context.Context, raw string) (*ResolvedURL, error) {
	current, err := r.normalizeAndValidate(raw)
	if err != nil {
		return nil, err
	}
	if resolved, err := ParseStravaURL(current.String()); err == nil {
		resolved.InputURL = raw
		return resolved, nil
	}

	for range maxRedirects {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, current.String(), http.NoBody)
		if err != nil {
			return nil, fmt.Errorf("create resolve request: %w", err)
		}
		req.Header.Set("User-Agent", resolveUA)
		req.Header.Set("Accept", "text/html,application/xhtml+xml;q=0.9,*/*;q=0.8")

		resp, err := r.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("resolve request: %w", err)
		}

		if loc := resp.Header.Get("Location"); loc != "" {
			next, err := resp.Request.URL.Parse(loc)
			if err != nil {
				_ = resp.Body.Close()
				return nil, fmt.Errorf("parse redirect location: %w", err)
			}
			if err := r.validateURL(next); err != nil {
				_ = resp.Body.Close()
				return nil, err
			}
			if resolved, err := ParseStravaURL(next.String()); err == nil {
				_ = resp.Body.Close()
				resolved.InputURL = raw
				return resolved, nil
			}
			_ = resp.Body.Close()
			current = next
			continue
		}

		body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxHTMLBytes))
		_ = resp.Body.Close()
		if readErr != nil {
			return nil, fmt.Errorf("read resolve body: %w", readErr)
		}
		if resolved, err := parseStravaURLFromHTML(string(body)); err == nil {
			resolved.InputURL = raw
			return resolved, nil
		}
		return nil, fmt.Errorf("could not resolve %q to a Strava activity, route, or segment", raw)
	}
	return nil, fmt.Errorf("too many redirects resolving %q", raw)
}

func parseStravaURLFromHTML(html string) (*ResolvedURL, error) {
	match := htmlResourceURLRe.FindString(html)
	if match == "" {
		return nil, errNotResourceURL
	}
	return ParseStravaURL(match)
}

func (r *URLResolver) normalizeAndValidate(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, errors.New("url is required")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("parse url: %w", err)
	}
	if u.Scheme == "" {
		u, err = url.Parse("https://" + raw)
		if err != nil {
			return nil, fmt.Errorf("parse url: %w", err)
		}
	}
	if err := r.validateURL(u); err != nil {
		return nil, err
	}
	return u, nil
}

func (r *URLResolver) validateURL(u *url.URL) error {
	if u == nil {
		return errors.New("invalid url")
	}
	switch u.Scheme {
	case "https":
	case "http":
		if !r.allowHTTP {
			return errors.New("refusing non-https url")
		}
	default:
		return fmt.Errorf("unsupported url scheme %q", u.Scheme)
	}
	if u.Host == "" {
		return errors.New("invalid url")
	}
	host := strings.ToLower(u.Hostname())
	if !r.allowedHosts[host] {
		return fmt.Errorf("refusing non-Strava host %q", host)
	}
	return nil
}

// APIPath returns the Strava API v3 path for the resolved resource.
func (r *ResolvedURL) APIPath() (string, error) {
	if r == nil || r.ID <= 0 {
		return "", errors.New("unresolved url")
	}
	switch r.Type {
	case ResourceActivity:
		return fmt.Sprintf("/activities/%d", r.ID), nil
	case ResourceRoute:
		return fmt.Sprintf("/routes/%d", r.ID), nil
	case ResourceSegment:
		return fmt.Sprintf("/segments/%d", r.ID), nil
	case ResourceAthlete:
		return fmt.Sprintf("/athletes/%d", r.ID), nil
	case ResourceClub:
		return fmt.Sprintf("/clubs/%d", r.ID), nil
	default:
		return "", fmt.Errorf("unsupported resource type %q", r.Type)
	}
}
