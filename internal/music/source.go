// Package music resolves public complete tracks and streams bounded decoded PCM.
// Session selection and device playback evidence belong to the caller.
package music

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

var (
	ErrUnavailable = errors.New("music_unavailable")
	ErrInvalid     = errors.New("music_invalid")
	ErrSource      = errors.New("music_source")
)

type Query struct{ Title, Artist string }
type Track struct {
	ID            int64
	Title, Artist string
	Duration      time.Duration
}

// Resolved contains a validated temporary resource. Its URL never leaves this package.
type Resolved struct {
	Track Track
	url   string
	size  int64
	track Track
}

// Keep temporary provider tokens out of incidental diagnostic formatting.
func (Resolved) String() string   { return "music_resource" }
func (Resolved) GoString() string { return "music_resource" }

type Catalog struct {
	client  *http.Client
	command func(context.Context) *exec.Cmd
}

const (
	maxMetadata    = 1 << 20
	maxSourceBytes = 50 << 20
	prepareTimeout = 15 * time.Second
	idleTimeout    = 10 * time.Second
)

// New creates an immutable, concurrency-safe catalog with no proxy, cookies or credentials.
func New() *Catalog {
	tr := &http.Transport{
		Proxy: nil, DialContext: secureDial(net.DefaultResolver.LookupNetIP, (&net.Dialer{Timeout: prepareTimeout}).DialContext),
		TLSClientConfig:     &tls.Config{MinVersion: tls.VersionTLS12},
		TLSHandshakeTimeout: prepareTimeout, ResponseHeaderTimeout: prepareTimeout,
		MaxResponseHeaderBytes: 32 << 10, MaxIdleConns: 4, MaxIdleConnsPerHost: 2, IdleConnTimeout: 30 * time.Second,
		TLSNextProto: map[string]func(string, *tls.Conn) http.RoundTripper{},
	}
	return &Catalog{client: &http.Client{Transport: tr, CheckRedirect: checkRedirect}, command: ffmpegCommand}
}

func (c *Catalog) Search(ctx context.Context, q Query) ([]Track, error) {
	q.Title, q.Artist = strings.TrimSpace(q.Title), strings.TrimSpace(q.Artist)
	query := strings.TrimSpace(q.Artist + " " + q.Title)
	if !validText(query, 120) || query == "" {
		return nil, ErrInvalid
	}
	u := url.URL{Scheme: "https", Host: "music.163.com", Path: "/api/search/get"}
	u.RawQuery = url.Values{"s": {query}, "type": {"1"}, "limit": {"20"}, "offset": {"0"}}.Encode()
	var response struct {
		Code   int `json:"code"`
		Result struct {
			Songs []struct {
				ID       int64  `json:"id"`
				Name     string `json:"name"`
				Duration int64  `json:"duration"`
				Artists  []struct {
					Name string `json:"name"`
				} `json:"artists"`
			} `json:"songs"`
		} `json:"result"`
	}
	if err := c.metadata(ctx, u.String(), &response); err != nil {
		return nil, err
	}
	if response.Code != 200 {
		return nil, ErrUnavailable
	}
	tracks := make([]Track, 0, 20)
	seen := make(map[int64]bool)
	for _, s := range response.Result.Songs {
		if len(tracks) == 20 {
			break
		}
		s.Name = strings.TrimSpace(s.Name)
		// Check milliseconds before multiplying to avoid duration overflow.
		if s.ID <= 0 || s.Name == "" || s.Duration < 1000 || s.Duration > int64((20*time.Minute)/time.Millisecond) || !validText(s.Name, 500) {
			continue
		}
		artistNames := make([]string, 0, len(s.Artists))
		matchedArtist := q.Artist == ""
		for _, a := range s.Artists {
			a.Name = strings.TrimSpace(a.Name)
			if !validText(a.Name, 200) || a.Name == "" {
				continue
			}
			artistNames = append(artistNames, a.Name)
			if matchesArtist(a.Name, q.Artist) {
				matchedArtist = true
			}
		}
		if len(artistNames) == 0 || !matchedArtist || !matchesTitle(s.Name, q.Title) || seen[s.ID] {
			continue
		}
		track := Track{ID: s.ID, Title: s.Name, Artist: strings.Join(artistNames, " / "), Duration: time.Duration(s.Duration) * time.Millisecond}
		if !validTrack(track) {
			continue
		}
		seen[s.ID] = true
		tracks = append(tracks, track)
	}
	if len(tracks) == 0 {
		return nil, ErrUnavailable
	}
	return tracks, nil
}

func (c *Catalog) Resolve(ctx context.Context, t Track) (Resolved, error) {
	if !validTrack(t) {
		return Resolved{}, ErrInvalid
	}
	u := url.URL{Scheme: "https", Host: "music.163.com", Path: "/api/song/enhance/player/url"}
	u.RawQuery = url.Values{"ids": {"[" + strconv.FormatInt(t.ID, 10) + "]"}, "br": {"320000"}}.Encode()
	var response struct {
		Code int `json:"code"`
		Data []struct {
			ID                 int64           `json:"id"`
			Code               int             `json:"code"`
			URL                string          `json:"url"`
			Size               int64           `json:"size"`
			Type               string          `json:"type"`
			FreeTrialInfo      json.RawMessage `json:"freeTrialInfo"`
			FreeTrialPrivilege struct {
				ResConsumable bool `json:"resConsumable"`
			} `json:"freeTrialPrivilege"`
		} `json:"data"`
	}
	if err := c.metadata(ctx, u.String(), &response); err != nil {
		return Resolved{}, err
	}
	if response.Code != 200 || len(response.Data) == 0 {
		return Resolved{}, ErrUnavailable
	}
	if len(response.Data) != 1 || response.Data[0].ID != t.ID {
		return Resolved{}, ErrSource
	}
	d := response.Data[0]
	if d.Code != 200 || d.URL == "" || (len(d.FreeTrialInfo) > 0 && string(d.FreeTrialInfo) != "null") || d.FreeTrialPrivilege.ResConsumable {
		return Resolved{}, ErrUnavailable
	}
	if d.Type != "mp3" || d.Size <= 0 || d.Size > maxSourceBytes {
		return Resolved{}, ErrSource
	}
	media, err := url.Parse(d.URL)
	if err != nil {
		return Resolved{}, ErrSource
	}
	// The public API may return HTTP. Upgrade without making an HTTP request.
	if media.Scheme == "http" {
		media.Scheme = "https"
	}
	if !validURL(media, false) {
		return Resolved{}, ErrSource
	}
	return Resolved{Track: t, url: media.String(), size: d.Size, track: t}, nil
}

func validTrack(t Track) bool {
	return t.ID > 0 && t.Duration >= time.Second && t.Duration <= 20*time.Minute && t.Title != "" && t.Artist != "" && validText(t.Title, 500) && validText(t.Artist, 2000)
}
func validText(s string, max int) bool {
	if !utf8.ValidString(s) || utf8.RuneCountInString(s) > max {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}
func normalizeName(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) || unicode.IsPunct(r) {
			return -1
		}
		return unicode.ToLower(r)
	}, s)
}
func matchesTitle(value, query string) bool {
	n := normalizeName(query)
	if query != "" {
		return n != "" && normalizeName(value) == n
	}
	// Artist selections exclude explicitly labelled alternate performances.
	// Metadata matching does not authenticate the recording or its performer.
	lower := strings.ToLower(value)
	for _, label := range []string{"现场", "伴奏", "翻唱", "混音", "串烧"} {
		if strings.Contains(lower, label) {
			return false
		}
	}
	for _, word := range strings.FieldsFunc(lower, func(r rune) bool { return r < 'a' || r > 'z' }) {
		if word == "live" || word == "cover" || word == "remix" {
			return false
		}
	}
	return true
}

// Each artist entry must match the requested name, not a fan's longer nickname.
// Collaborations remain valid when one full artist entry matches.
func matchesArtist(value, query string) bool {
	n := normalizeName(query)
	return query == "" || (n != "" && normalizeName(value) == n)
}
func (c *Catalog) metadata(ctx context.Context, uri string, out any) error {
	ctx, cancel := context.WithTimeout(ctx, prepareTimeout)
	defer cancel()
	r, err := c.get(ctx, uri, true)
	if err != nil {
		return err
	}
	defer r.Body.Close()
	if r.StatusCode != http.StatusOK {
		return ErrUnavailable
	}
	if r.ContentLength > maxMetadata {
		return ErrSource
	}
	b, err := io.ReadAll(io.LimitReader(r.Body, maxMetadata+1))
	if err != nil {
		return sourceError(ctx)
	}
	if len(b) > maxMetadata || json.Unmarshal(b, out) != nil {
		return ErrSource
	}
	return nil
}
func (c *Catalog) get(ctx context.Context, uri string, api bool) (*http.Response, error) {
	u, err := url.Parse(uri)
	if err != nil || !validURL(u, api) {
		return nil, ErrSource
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, ErrSource
	}
	req.Header.Set("User-Agent", "family-robot-music-probe/1.0")
	req.Header.Set("Accept-Encoding", "identity")
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, sourceError(ctx)
	}
	return resp, nil
}
func sourceError(ctx context.Context) error {
	if ctx.Err() != nil {
		return sourceFailure{ctx.Err()}
	}
	return ErrSource
}

type sourceFailure struct{ cause error }

func (sourceFailure) Error() string     { return "music_source" }
func (e sourceFailure) Unwrap() []error { return []error{ErrSource, e.cause} }
func validURL(u *url.URL, api bool) bool {
	if u == nil || u.Scheme != "https" || u.User != nil || u.Fragment != "" || u.Opaque != "" || (u.Port() != "" && u.Port() != "443") {
		return false
	}
	host := strings.ToLower(u.Hostname())
	if api {
		return host == "music.163.com" && (u.Path == "/api/search/get" || u.Path == "/api/song/enhance/player/url")
	}
	return host == "music.126.net" || strings.HasSuffix(host, ".music.126.net")
}
func checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) > 3 || len(via) == 0 {
		return ErrSource
	}
	api := strings.EqualFold(via[0].URL.Hostname(), "music.163.com")
	if !validURL(req.URL, api) {
		return ErrSource
	}
	return nil
}

type lookupIP func(context.Context, string, string) ([]netip.Addr, error)
type dialIP func(context.Context, string, string) (net.Conn, error)

// Resolve once and dial a checked literal IP. TLS still verifies the original
// hostname; a later connection resolves and checks again to prevent DNS rebinding.
func secureDial(lookup lookupIP, dial dialIP) dialIP {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil || port != "443" {
			return nil, ErrSource
		}
		if host != "music.163.com" && host != "music.126.net" && !strings.HasSuffix(host, ".music.126.net") {
			return nil, ErrSource
		}
		ips, err := lookup(ctx, "ip", host)
		if err != nil || len(ips) == 0 {
			return nil, ErrSource
		}
		for _, ip := range ips {
			if !publicIP(ip) {
				return nil, ErrSource
			}
		}
		for _, ip := range ips {
			if conn, e := dial(ctx, network, net.JoinHostPort(ip.String(), port)); e == nil {
				return conn, nil
			}
		}
		return nil, ErrSource
	}
}

var deniedPrefixes = func() []netip.Prefix {
	var out []netip.Prefix
	for _, s := range []string{"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16", "172.16.0.0/12", "192.0.0.0/24", "192.0.2.0/24", "192.88.99.0/24", "192.168.0.0/16", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "224.0.0.0/4", "240.0.0.0/4", "2001::/23", "2001:db8::/32", "2002::/16"} {
		out = append(out, netip.MustParsePrefix(s))
	}
	return out
}()

func publicIP(ip netip.Addr) bool {
	if !ip.IsValid() || ip.Zone() != "" {
		return false
	}
	ip = ip.Unmap()
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return false
	}
	if ip.Is6() && !netip.MustParsePrefix("2000::/3").Contains(ip) {
		return false
	}
	for _, p := range deniedPrefixes {
		if p.Contains(ip) {
			return false
		}
	}
	return true
}
