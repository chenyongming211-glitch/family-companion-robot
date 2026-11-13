package management

import (
	"bytes"
	"context"
	"encoding/json"
	"familyrobot/internal/music"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestMusicLibraryAuthorizationAndMethods(t *testing.T) {
	s, _ := httpFixture(t)
	s.opts.MusicLibraryRoot = t.TempDir()
	s.cfg.Accounts[0].Memberships[0].CanAddDevices = true
	p := "/api/family/music-library/" + s.cfg.Devices[0].ID
	if w := request(s, "GET", p, "", nil); w.Code != 401 {
		t.Fatal(w.Code)
	}
	c := login(t, s, "family", "alice")
	if w := request(s, "GET", p, "", c); w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	if w := request(s, "PUT", p, "", c); w.Code != 405 {
		t.Fatal(w.Code)
	}
	if w := request(s, "DELETE", p+"?id=1", "", c); w.Code != 400 {
		t.Fatal(w.Code)
	}
	s.opts.MusicLibraryRoot = ""
	if w := request(s, "GET", p, "", c); w.Code != 404 {
		t.Fatal(w.Code)
	}
}

func musicUpload(t *testing.T, s *Server, c *http.Cookie, data []byte, fields [][2]string, tail bool, change func()) *httptest.ResponseRecorder {
	t.Helper()
	var b bytes.Buffer
	m := multipart.NewWriter(&b)
	for _, v := range fields {
		if e := m.WriteField(v[0], v[1]); e != nil {
			t.Fatal(e)
		}
	}
	p, e := m.CreateFormFile("file", "synthetic.mp3")
	if e != nil {
		t.Fatal(e)
	}
	p.Write(data)
	if tail {
		m.WriteField("unexpected", "tail")
	}
	m.Close()
	r := httptest.NewRequest("POST", "http://localhost:18791/api/family/music-library/"+s.cfg.Devices[0].ID, &b)
	r.Header.Set("Content-Type", m.FormDataContentType())
	r.AddCookie(c)
	if change != nil {
		r.Body = &musicChangingBody{ReadCloser: r.Body, change: change}
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}

type musicChangingBody struct {
	io.ReadCloser
	change func()
}

func (b *musicChangingBody) Read(p []byte) (int, error) {
	n, e := b.ReadCloser.Read(p)
	if b.change != nil {
		b.change()
		b.change = nil
	}
	return n, e
}
func TestMusicLibraryImportEnvelopeAndLiveScope(t *testing.T) {
	s, _ := httpFixture(t)
	s.opts.MusicLibraryRoot = t.TempDir()
	s.cfg.Accounts[0].Memberships[0].CanAddDevices = true
	c := login(t, s, "family", "alice")
	fields := [][2]string{{"title", "Synthetic"}, {"artist", "Test"}, {"personalUse", "true"}}
	data, e := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "sine=frequency=440:duration=1.2", "-f", "mp3", "pipe:1").Output()
	if e != nil {
		t.Fatal(e)
	}
	for _, tc := range []struct {
		name   string
		fields [][2]string
		tail   bool
		data   []byte
	}{
		{"tail", fields, true, data}, {"duplicate", append(append([][2]string{}, fields...), [2]string{"title", "again"}), false, data}, {"unknown", append(append([][2]string{}, fields...), [2]string{"other", "x"}), false, data}, {"confirmation", [][2]string{{"title", "s"}, {"artist", "t"}, {"personalUse", "false"}}, false, data}, {"fake", fields, false, []byte("fake mp3")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := musicUpload(t, s, c, tc.data, tc.fields, tc.tail, nil)
			if w.Code == 200 {
				t.Fatal(w.Body)
			}
			l, _ := music.NewLibrary(s.opts.MusicLibraryRoot, "home-a")
			items, e := l.WithAccessCheck(func() bool { return true }).List()
			if e != nil || len(items) != 0 {
				t.Fatal(items, e)
			}
		})
	}
	w := musicUpload(t, s, c, data, fields, false, nil)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	var track music.LibraryTrack
	if json.Unmarshal(w.Body.Bytes(), &track) != nil || track.ID == 0 {
		t.Fatal(w.Body)
	}
	p := "/api/family/music-library/" + s.cfg.Devices[0].ID
	if w := request(s, "GET", p, "", c); w.Code != 200 || !strings.Contains(w.Body.String(), "Synthetic") {
		t.Fatal(w.Code, w.Body)
	}
	l, _ := music.NewLibrary(s.opts.MusicLibraryRoot, "home-a")
	l = l.WithAccessCheck(func() bool { return true })
	catalog := music.New().WithLibrary(l, func() bool { return true })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tracks, e := catalog.Search(ctx, music.Query{Title: "Synthetic", Artist: "Test"})
	if e != nil || len(tracks) != 1 {
		t.Fatal(tracks, e)
	}
	resolved, e := catalog.Resolve(ctx, tracks[0])
	if e != nil {
		t.Fatal(e)
	}
	var pcm int
	_, e = catalog.Stream(ctx, resolved, func(p []byte) error { pcm += len(p); return nil })
	if e != nil || pcm == 0 {
		t.Fatal(pcm, e)
	}
	if w := request(s, "DELETE", p+"?id="+strconv.FormatInt(track.ID, 10), "", c); w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	if _, e := catalog.Resolve(ctx, tracks[0]); e == nil {
		t.Fatal("deleted song resolved")
	}
	w = musicUpload(t, s, c, data, fields, false, func() { s.cfg.Bindings[0].Version++ })
	if w.Code == 200 {
		t.Fatal("binding mutation accepted")
	}
}

func TestMusicLibraryScopeDenials(t *testing.T) {
	s, _ := httpFixture(t)
	s.opts.MusicLibraryRoot = t.TempDir()
	p := "/api/family/music-library/" + s.cfg.Devices[0].ID
	for _, v := range []struct {
		scope, user, method string
		want                int
	}{{"family", "alice", "POST", 403}, {"family", "alice", "DELETE", 403}, {"family", "bob", "GET", 403}, {"admin", "observer", "GET", 403}, {"admin", "observer", "POST", 403}} {
		c := login(t, s, v.scope, v.user)
		path := strings.Replace(p, "/family/", "/"+v.scope+"/", 1)
		if w := request(s, v.method, path, "malformed", c); w.Code != v.want {
			t.Fatalf("%+v %d %s", v, w.Code, w.Body)
		}
	}
	c := login(t, s, "family", "metadata")
	if w := request(s, "GET", p, "", c); w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	// Metadata-only grant remains readable; management requires the explicit family grant.
	s.cfg.Accounts[3].Memberships[0].CanAddDevices = true
	if w := request(s, "DELETE", p+"?id=1", "", c); w.Code != 400 {
		t.Fatal(w.Code, w.Body)
	}
	if w := request(s, "GET", p+"/audio", "", c); w.Code == 200 {
		t.Fatal("download route")
	}
}
func TestMusicLibraryUploadBoundAndCancellation(t *testing.T) {
	s, _ := httpFixture(t)
	s.opts.MusicLibraryRoot = t.TempDir()
	s.cfg.Accounts[0].Memberships[0].CanAddDevices = true
	c := login(t, s, "family", "alice")
	fields := [][2]string{{"title", "Synthetic"}, {"artist", "Test"}, {"personalUse", "true"}}
	if w := musicUpload(t, s, c, bytes.Repeat([]byte{'x'}, int(music.MaxLibraryTrackBytes)+1), fields, false, nil); w.Code == 200 {
		t.Fatal("oversized accepted")
	}
	var b bytes.Buffer
	m := multipart.NewWriter(&b)
	for _, v := range fields {
		m.WriteField(v[0], v[1])
	}
	part, _ := m.CreateFormFile("file", "x.mp3")
	part.Write([]byte("fake"))
	m.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := httptest.NewRequest("POST", "http://localhost:18791/api/family/music-library/"+s.cfg.Devices[0].ID, &b).WithContext(ctx)
	r.Header.Set("Content-Type", m.FormDataContentType())
	r.AddCookie(c)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code == 200 {
		t.Fatal("canceled accepted")
	}
	l, _ := music.NewLibrary(s.opts.MusicLibraryRoot, "home-a")
	items, e := l.WithAccessCheck(func() bool { return true }).List()
	if e != nil || len(items) != 0 {
		t.Fatal(items, e)
	}
}

func TestMusicLibraryRejectsBusyUploadsBeforeBody(t *testing.T) {
	s, _ := httpFixture(t)
	s.opts.MusicLibraryRoot = t.TempDir()
	s.cfg.Accounts[0].Memberships[0].CanAddDevices = true
	c := login(t, s, "family", "alice")
	s.musicUploads <- struct{}{}
	s.musicUploads <- struct{}{}
	w := request(s, "POST", "/api/family/music-library/"+s.cfg.Devices[0].ID, "malformed", c)
	if w.Code != 429 {
		t.Fatal(w.Code, w.Body)
	}
}
