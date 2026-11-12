package music

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func fixtureCatalog(t *testing.T, response string, check func(*http.Request)) *Catalog {
	t.Helper()
	c := New()
	c.client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if check != nil {
			check(r)
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(response)), Request: r}, nil
	})}
	return c
}

func TestSearchMatchesRequestedTitleAndArtist(t *testing.T) {
	c := fixtureCatalog(t, `{"code":200,"result":{"songs":[{"id":1,"name":"黄昏","duration":344000,"artists":[{"name":"周传雄"}]},{"id":2,"name":"别的歌","duration":200000,"artists":[{"name":"周传雄"}]},{"id":3,"name":"黄昏","duration":200000,"artists":[{"name":"另一人"}]}]}}`, func(r *http.Request) {
		if r.Method != "GET" || r.URL.Host != "music.163.com" || r.URL.Path != "/api/search/get" || r.URL.Query().Get("s") != "周传雄 黄昏" {
			t.Fatalf("unexpected search request")
		}
		if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
			t.Fatal("credentialed request")
		}
	})
	tracks, err := c.Search(context.Background(), Query{Title: " 黄昏 ", Artist: "周传雄"})
	if err != nil || len(tracks) != 1 || tracks[0].ID != 1 || tracks[0].Duration != 344*time.Second || tracks[0].Artist != "周传雄" {
		t.Fatalf("tracks=%+v err=%v", tracks, err)
	}
}

func TestResolveAcceptsCompleteMatchingMP3(t *testing.T) {
	c := fixtureCatalog(t, `{"code":200,"data":[{"id":5255640,"code":200,"url":"http://m701.music.126.net/valid.mp3","size":5508328,"type":"mp3","freeTrialInfo":null}]}`, func(r *http.Request) {
		if r.URL.Path != "/api/song/enhance/player/url" || r.URL.Query().Get("ids") != "[5255640]" || r.URL.Query().Get("br") != "128000" {
			t.Fatal("unexpected resolve request")
		}
	})
	r, err := c.Resolve(context.Background(), Track{ID: 5255640, Title: "黄昏", Artist: "周传雄", Duration: 344 * time.Second})
	if err != nil || r.Track.ID != 5255640 || r.size != 5508328 || !strings.HasPrefix(r.url, "https://") {
		t.Fatalf("resolve failed: %v", err)
	}
}

func TestResolveRejectsUnavailablePreviewAndMalformedSource(t *testing.T) {
	for _, tc := range []struct {
		name, fields string
		want         error
	}{
		{"unavailable", `"id":1,"code":404,"url":null,"size":0,"type":"mp3"`, ErrUnavailable},
		{"preview", `"id":1,"code":200,"url":"https://music.126.net/a.mp3","size":1000,"type":"mp3","freeTrialInfo":{"start":0,"end":30}`, ErrUnavailable},
		{"wrong id", `"id":2,"code":200,"url":"https://music.126.net/a.mp3","size":1000,"type":"mp3"`, ErrSource},
		{"wrong type", `"id":1,"code":200,"url":"https://music.126.net/a.mp3","size":1000,"type":"flac"`, ErrSource},
		{"oversize", `"id":1,"code":200,"url":"https://music.126.net/a.mp3","size":52428801,"type":"mp3"`, ErrSource},
		{"empty size", `"id":1,"code":200,"url":"https://music.126.net/a.mp3","size":0,"type":"mp3"`, ErrSource},
		{"host trick", `"id":1,"code":200,"url":"https://music.126.net.evil.test/a.mp3","size":1000,"type":"mp3"`, ErrSource},
		{"userinfo", `"id":1,"code":200,"url":"https://user@music.126.net/a.mp3","size":1000,"type":"mp3"`, ErrSource},
		{"port", `"id":1,"code":200,"url":"https://music.126.net:444/a.mp3","size":1000,"type":"mp3"`, ErrSource},
		{"fragment", `"id":1,"code":200,"url":"https://music.126.net/a.mp3#x","size":1000,"type":"mp3"`, ErrSource},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := fixtureCatalog(t, `{"code":200,"data":[{`+tc.fields+`}]}`, nil)
			_, err := c.Resolve(context.Background(), Track{ID: 1, Title: "song", Artist: "artist", Duration: time.Minute})
			if !errors.Is(err, tc.want) || strings.Contains(err.Error(), "https:") {
				t.Fatalf("got %v want %v", err, tc.want)
			}
		})
	}
}

func TestInputAndMetadataBounds(t *testing.T) {
	c := fixtureCatalog(t, strings.Repeat("x", (1<<20)+1), nil)
	for _, q := range []Query{{}, {Title: strings.Repeat("歌", 121)}, {Title: "bad\x00query"}} {
		if _, e := c.Search(context.Background(), q); !errors.Is(e, ErrInvalid) {
			t.Errorf("query not rejected: %v", e)
		}
	}
	if _, e := c.Search(context.Background(), Query{Artist: "artist"}); !errors.Is(e, ErrSource) {
		t.Errorf("oversize metadata: %v", e)
	}
	for _, d := range []time.Duration{0, time.Second - time.Nanosecond, 20*time.Minute + time.Nanosecond} {
		if _, e := c.Resolve(context.Background(), Track{ID: 1, Duration: d}); !errors.Is(e, ErrInvalid) {
			t.Errorf("duration accepted: %v", e)
		}
	}
}

func TestSearchCandidateLimitAndMetadataValidation(t *testing.T) {
	var entries []string
	for i := 1; i <= 30; i++ {
		entries = append(entries, fmt.Sprintf(`{"id":%d,"name":"Song","duration":2000,"artists":[{"name":"Artist"}]}`, i))
	}
	c := fixtureCatalog(t, `{"code":200,"result":{"songs":[`+strings.Join(entries, ",")+`]}}`, nil)
	list, e := c.Search(context.Background(), Query{Artist: "artist"})
	if e != nil || len(list) != 20 {
		t.Fatalf("candidate cap: %d %v", len(list), e)
	}
	c = fixtureCatalog(t, `{"code":200,"result":{"songs":[{"id":1,"name":"Song","duration":9223372036854775807,"artists":[{"name":"Artist"}]},{"id":2,"name":"Song","duration":2000,"artists":[]}]}}`, nil)
	if _, e = c.Search(context.Background(), Query{Title: "Song"}); !errors.Is(e, ErrUnavailable) {
		t.Fatalf("invalid candidates: %v", e)
	}
	if _, e = c.Search(context.Background(), Query{Title: strings.Repeat("歌", 60), Artist: strings.Repeat("人", 60)}); !errors.Is(e, ErrInvalid) {
		t.Fatalf("combined query limit: %v", e)
	}
}

func TestURLAndRedirectAllowlist(t *testing.T) {
	for _, s := range []string{"http://music.126.net/x", "https://music.126.net.evil/x", "https://evil.music.126.net@127.0.0.1/x", "https://127.0.0.1/x", "https://music.126.net:80/x", "https://music.126.net/x#fragment", "https://music.163.com/api/search/get"} {
		u, e := url.Parse(s)
		if e == nil && validURL(u, false) {
			t.Fatalf("accepted %s", s)
		}
	}
	first, _ := http.NewRequest("GET", "https://music.126.net/a", nil)
	next, _ := http.NewRequest("GET", "https://m10.music.126.net/b", nil)
	for n := 1; n <= 3; n++ {
		via := make([]*http.Request, n)
		for i := range via {
			via[i] = first
		}
		if e := checkRedirect(next, via); e != nil {
			t.Errorf("allowed redirect %d: %v", n, e)
		}
	}
	if e := checkRedirect(next, []*http.Request{first, first, first, first}); e == nil {
		t.Fatal("fourth redirect allowed")
	}
	next, _ = http.NewRequest("GET", "https://music.163.com/api/search/get", nil)
	if e := checkRedirect(next, []*http.Request{first}); e == nil {
		t.Fatal("media redirected to API")
	}
	api, _ := http.NewRequest("GET", "https://music.163.com/api/search/get", nil)
	if e := checkRedirect(first, []*http.Request{api}); e == nil {
		t.Fatal("API redirected to media")
	}
}

func TestSecureDialRejectsPrivateReservedAndMixedDNS(t *testing.T) {
	bad := []string{"127.0.0.1", "10.0.0.1", "172.16.1.1", "192.168.1.1", "169.254.169.254", "100.64.0.1", "0.0.0.0", "192.0.2.1", "198.18.0.1", "198.51.100.1", "203.0.113.1", "224.0.0.1", "240.0.0.1", "::1", "::ffff:127.0.0.1", "fc00::1", "fe80::1", "2001:db8::1", "2002:7f00:1::1"}
	for _, ip := range bad {
		t.Run(ip, func(t *testing.T) {
			dialed := false
			d := secureDial(func(context.Context, string, string) ([]netip.Addr, error) {
				return []netip.Addr{netip.MustParseAddr("1.1.1.1"), netip.MustParseAddr(ip)}, nil
			}, func(context.Context, string, string) (net.Conn, error) { dialed = true; return nil, nil })
			_, e := d(context.Background(), "tcp", "m10.music.126.net:443")
			if e == nil || dialed {
				t.Fatalf("private DNS allowed: %v %v", e, dialed)
			}
		})
	}
}

func TestSecureDialPinsPublicIPAndRechecksEveryConnection(t *testing.T) {
	calls := 0
	d := secureDial(func(context.Context, string, string) ([]netip.Addr, error) {
		calls++
		if calls > 1 {
			return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
		}
		return []netip.Addr{netip.MustParseAddr("1.1.1.1")}, nil
	}, func(_ context.Context, _ string, address string) (net.Conn, error) {
		if address != "1.1.1.1:443" {
			t.Fatalf("DNS used again: %s", address)
		}
		a, b := net.Pipe()
		b.Close()
		return a, nil
	})
	conn, e := d(context.Background(), "tcp", "music.126.net:443")
	if e != nil {
		t.Fatal(e)
	}
	conn.Close()
	if _, e = d(context.Background(), "tcp", "music.126.net:443"); e == nil {
		t.Fatal("rebinding accepted")
	}
}

func TestSearchRejectsEmptyNamesAndOversizedCombinedArtist(t *testing.T) {
	c := fixtureCatalog(t, `{"code":200,"result":{"songs":[{"id":1,"name":"   ","duration":2000,"artists":[{"name":"Artist"}]}]}}`, nil)
	if _, e := c.Search(context.Background(), Query{Artist: "Artist"}); !errors.Is(e, ErrUnavailable) {
		t.Fatalf("empty title accepted: %v", e)
	}
	artist := strings.Repeat("a", 200)
	c = fixtureCatalog(t, `{"code":200,"result":{"songs":[{"id":1,"name":"Song","duration":2000,"artists":[`+strings.TrimSuffix(strings.Repeat(`{"name":"`+artist+`"},`, 11), ",")+`]}]}}`, nil)
	if _, e := c.Search(context.Background(), Query{Title: "Song"}); !errors.Is(e, ErrUnavailable) {
		t.Fatalf("oversized artist accepted: %v", e)
	}
}

func TestHTTPRedirectsRejectOutsideAllowlistWithoutFetchingTarget(t *testing.T) {
	c := New()
	calls := 0
	c.client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{"https://127.0.0.1/private"}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	})
	_, e := c.Search(context.Background(), Query{Title: "Song"})
	if !errors.Is(e, ErrSource) || calls != 1 || e.Error() != "music_source" {
		t.Fatalf("redirect err=%v calls=%d", e, calls)
	}
}

func TestTransportErrorDetailsNeverEscape(t *testing.T) {
	c := New()
	c.client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("secret https://private/body") })
	_, e := c.Search(context.Background(), Query{Title: "Song"})
	if !errors.Is(e, ErrSource) || e.Error() != "music_source" {
		t.Fatalf("unsafe error: %v", e)
	}
}

func TestResolvedFormattingDoesNotExposeTemporaryURL(t *testing.T) {
	r := Resolved{Track: Track{ID: 1}, url: "https://music.126.net/private-token"}
	for _, s := range []string{fmt.Sprint(r), fmt.Sprintf("%+v", r), fmt.Sprintf("%#v", r)} {
		if strings.Contains(s, "private-token") || strings.Contains(s, "https:") {
			t.Fatal("temporary URL exposed by formatting")
		}
	}
}
