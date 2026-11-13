package music

import (
	"context"
	"errors"
	"net/http"
	"testing"
)

type rejectOnline struct{ calls int }

func (r *rejectOnline) RoundTrip(*http.Request) (*http.Response, error) {
	r.calls++
	return nil, errors.New("unexpected online request")
}
func TestPrivateOnlyNeverFallsBack(t *testing.T) {
	c := NewPrivateOnly()
	transport := &rejectOnline{}
	c.client = &http.Client{Transport: transport}
	l, e := NewLibrary(t.TempDir(), "family-a")
	if e != nil {
		t.Fatal(e)
	}
	c = c.WithLibrary(l.WithAccessCheck(func() bool { return true }), func() bool { return true })
	if _, e = c.Search(context.Background(), Query{Title: "missing"}); !errors.Is(e, ErrUnavailable) {
		t.Fatal(e)
	}
	if _, e = c.Resolve(context.Background(), Track{ID: 1, Title: "song", Artist: "artist"}); e == nil {
		t.Fatal("online resolve accepted")
	}
	if transport.calls != 0 || c.OnlineEnabled() || !New().OnlineEnabled() {
		t.Fatal("private-only sent provider request", transport.calls)
	}
	online := New()
	online.client = &http.Client{Transport: transport}
	online = online.WithLibrary(l.WithAccessCheck(func() bool { return true }), func() bool { return true })
	online.Search(context.Background(), Query{Title: "missing"})
	if transport.calls != 1 {
		t.Fatal("explicit online fallback lost", transport.calls)
	}
}
