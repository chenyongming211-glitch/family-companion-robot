package management

import (
	"context"
	"errors"
	"familyrobot/internal/music"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func musicError(w http.ResponseWriter, e error) {
	switch {
	case errors.Is(e, music.ErrInvalid):
		sourceError(w, ErrInvalid)
	case errors.Is(e, music.ErrLibraryForbidden):
		sourceError(w, ErrForbidden)
	case errors.Is(e, music.ErrLibraryFull):
		apiError(w, 409, "music_library_full", "曲库空间已满")
	default:
		sourceError(w, ErrSource)
	}
}
func sameMusicBinding(a, b Binding) bool {
	return a.ID == b.ID && a.Version == b.Version && a.FamilyID == b.FamilyID && a.DeviceID == b.DeviceID && a.From.Equal(b.From)
}
func (s *Server) musicLibrary(w http.ResponseWriter, r *http.Request, a AccountConfig, parts []string) {
	if s.opts.MusicLibraryRoot == "" {
		sourceError(w, ErrNotFound)
		return
	}
	if len(parts) != 3 || !device.MatchString(parts[2]) {
		sourceError(w, ErrInvalid)
		return
	}
	if r.Method != "GET" && r.Method != "POST" && r.Method != "DELETE" {
		apiError(w, 405, "method_not_allowed", "请求方法不支持")
		return
	}
	id, scope := parts[2], parts[0]
	c, e := s.snapshot()
	if e != nil {
		sourceError(w, e)
		return
	}
	binding, can, e := runtimeScopePermission(c, a.ID, scope, id, s.now())
	if e != nil {
		sourceError(w, e)
		return
	}
	mutation := r.Method != "GET"
	if mutation && !can {
		sourceError(w, ErrForbidden)
		return
	}
	allowed := func() bool {
		fresh, e := s.snapshot()
		if e != nil {
			return false
		}
		rr := r.WithContext(context.WithValue(r.Context(), configContextKey{}, fresh))
		current, ok := s.account(rr, scope)
		if !ok || current.ID != a.ID {
			return false
		}
		b, manage, e := runtimeScopePermission(fresh, a.ID, scope, id, s.now())
		return e == nil && sameMusicBinding(binding, b) && (!mutation || manage) && manage == can && r.Context().Err() == nil
	}
	l, e := music.NewLibrary(s.opts.MusicLibraryRoot, binding.FamilyID)
	if e != nil {
		musicError(w, e)
		return
	}
	l = l.WithAccessCheck(allowed)
	switch r.Method {
	case "GET":
		if r.URL.RawQuery != "" {
			sourceError(w, ErrInvalid)
			return
		}
		items, e := l.List()
		if e != nil {
			musicError(w, e)
			return
		}
		var used int64
		for _, v := range items {
			used += v.Bytes
		}
		if !allowed() {
			sourceError(w, ErrForbidden)
			return
		}
		writeJSON(w, map[string]any{"deviceId": id, "items": items, "canManage": can, "maxBytes": music.MaxLibraryBytes, "usedBytes": used, "maxTrackBytes": music.MaxLibraryTrackBytes})
	case "DELETE":
		q, queryErr := url.ParseQuery(r.URL.RawQuery)
		raw := q.Get("id")
		n, err := strconv.ParseInt(raw, 10, 64)
		if queryErr != nil || err != nil || strconv.FormatInt(n, 10) != raw || len(q) != 1 || len(q["id"]) != 1 || !music.IsPrivateTrack(music.Track{ID: n}) {
			sourceError(w, ErrInvalid)
			return
		}
		if e := l.Delete(n); e != nil {
			musicError(w, e)
			return
		}
		if !allowed() {
			sourceError(w, ErrForbidden)
			return
		}
		writeJSON(w, map[string]int64{"deleted": n})
	case "POST":
		if r.URL.RawQuery != "" {
			sourceError(w, ErrInvalid)
			return
		}
		select {
		case s.musicUploads <- struct{}{}:
			defer func() { <-s.musicUploads }()
		default:
			apiError(w, 429, "upload_busy", "请稍后重试")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
		defer cancel()
		body := r.Body
		stop := context.AfterFunc(ctx, func() { body.Close() })
		defer stop()
		deadline := time.Now().Add(120 * time.Second)
		rc := http.NewResponseController(w)
		for _, e := range []error{rc.SetReadDeadline(deadline), rc.SetWriteDeadline(deadline)} {
			if e != nil && !errors.Is(e, http.ErrNotSupported) {
				sourceError(w, ErrSource)
				return
			}
		}
		r.Body = http.MaxBytesReader(w, r.Body, music.MaxLibraryTrackBytes+(32<<10))
		mr, e := r.MultipartReader()
		if e != nil {
			sourceError(w, ErrInvalid)
			return
		}
		fields := map[string]string{}
		for {
			p, e := mr.NextPart()
			if e != nil {
				sourceError(w, ErrInvalid)
				return
			}
			name := p.FormName()
			if name == "file" {
				if len(fields) != 3 || fields["personalUse"] != "true" || p.FileName() == "" {
					sourceError(w, ErrInvalid)
					return
				}
				format := strings.TrimPrefix(strings.ToLower(filepath.Ext(p.FileName())), ".")
				track, e := l.Import(ctx, fields["title"], fields["artist"], format, &musicFinalPart{part: p, reader: mr, body: r.Body})
				if e != nil {
					musicError(w, e)
					return
				}
				if !allowed() {
					sourceError(w, ErrForbidden)
					return
				}
				writeJSON(w, track)
				return
			}
			if name != "title" && name != "artist" && name != "personalUse" {
				sourceError(w, ErrInvalid)
				return
			}
			if _, ok := fields[name]; ok || p.FileName() != "" {
				sourceError(w, ErrInvalid)
				return
			}
			data, e := io.ReadAll(io.LimitReader(p, 2049))
			if e != nil || len(data) > 2048 {
				sourceError(w, ErrInvalid)
				return
			}
			fields[name] = string(data)
		}
	}
}

// Verify the entire multipart envelope before Import can atomically commit.
type musicFinalPart struct {
	part   *multipart.Part
	reader *multipart.Reader
	body   io.Reader
	done   bool
}

func (r *musicFinalPart) Read(p []byte) (int, error) {
	n, e := r.part.Read(p)
	if e == io.EOF && !r.done {
		r.done = true
		if _, tail := r.reader.NextPart(); tail != io.EOF {
			return n, music.ErrInvalid
		}
		// NextPart may consume buffered epilogue; bound and drain remaining bytes to
		// enforce the HTTP body limit and detect transport cancellation before commit.
		if _, tail := io.Copy(io.Discard, r.body); tail != nil {
			return n, music.ErrInvalid
		}
	}
	return n, e
}
