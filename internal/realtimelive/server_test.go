package realtimelive

import (
	"encoding/json"
	"github.com/gorilla/websocket"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestTicketSingleUseOriginExpiryAndAdmission(t *testing.T) {
	s := NewServer(Config{}, nil, t.TempDir(), "test")
	s.run = func(ws *websocket.Conn, dir string) { defer ws.Close(); ws.ReadMessage() }
	h := httptest.NewServer(s.Handler())
	defer h.Close()
	pair := func() string {
		r, e := http.Post(h.URL+"/pair", "application/json", nil)
		if e != nil {
			t.Fatal(e)
		}
		defer r.Body.Close()
		var v map[string]string
		json.NewDecoder(r.Body).Decode(&v)
		return v["ticket"]
	}
	req, _ := http.NewRequest("POST", h.URL+"/pair", nil)
	req.Header.Set("Origin", "http://evil")
	resp, _ := http.DefaultClient.Do(req)
	resp.Body.Close()
	if resp.StatusCode != 409 {
		t.Fatal(resp.StatusCode)
	}
	token := pair()
	expired := pair()
	s.mu.Lock()
	binding := s.tickets[expired]
	binding.expiry = time.Now().Add(-time.Second)
	s.tickets[expired] = binding
	s.mu.Unlock()
	dial := func(token string) (*websocket.Conn, *http.Response, error) {
		return websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(h.URL, "http")+"/live", http.Header{"Authorization": []string{"Bearer " + token}})
	}
	for _, v := range []string{token, expired} {
		c, r, e := dial(v)
		if c != nil {
			c.Close()
		}
		if e == nil || r.StatusCode != 403 {
			t.Fatal("invalid ticket accepted")
		}
	}
	token = pair()
	c, _, e := dial(token)
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	resp, _ = http.Post(h.URL+"/pair", "application/json", nil)
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 409 {
		t.Fatal("busy pair accepted")
	}
	c.Close()
	for range 100 {
		r, _ := http.Get(h.URL + "/healthz")
		var m map[string]any
		json.NewDecoder(r.Body).Decode(&m)
		r.Body.Close()
		if m["busy"] == false {
			break
		}
		time.Sleep(time.Millisecond)
	}
	c2, r, e := dial(token)
	if c2 != nil {
		c2.Close()
	}
	if e == nil || r.StatusCode != 403 {
		t.Fatal("reused ticket")
	}
}
