package realtimelive

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

func randomID() string {
	var b [16]byte
	if _, e := rand.Read(b[:]); e != nil {
		panic(e)
	}
	return hex.EncodeToString(b[:])
}
func privateJSON(path string, v any) error {
	b, e := json.Marshal(v)
	if e != nil {
		return e
	}
	f, e := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	_, e = f.Write(b)
	ce := f.Close()
	if e == nil {
		e = ce
	}
	return e
}

type intent struct {
	path  string
	data  map[string]any
	start time.Time
}

func newIntent(dir, stage string, synthetic bool) (*intent, error) {
	i := &intent{filepath.Join(dir, stage+"-"+randomID()+".json"), map[string]any{"stage": stage, "state": "intent_saved", "synthetic_only": synthetic}, time.Now()}
	return i, i.update(nil)
}
func (i *intent) update(fields map[string]any) error {
	for k, v := range fields {
		i.data[k] = v
	}
	i.data["elapsed_ms"] = time.Since(i.start).Milliseconds()
	return privateJSON(i.path, i.data)
}

type recorder struct {
	mu  sync.Mutex
	dir string
}

func (r *recorder) add(event string, fields map[string]any) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	m := map[string]any{"event": event, "at": float64(time.Now().UnixNano()) / 1e9}
	for k, v := range fields {
		m[k] = v
	}
	b, e := json.Marshal(m)
	if e != nil {
		return e
	}
	b = append(b, '\n')
	f, e := os.OpenFile(filepath.Join(r.dir, "events.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	_, e = f.Write(b)
	ce := f.Close()
	if e == nil {
		e = ce
	}
	return e
}
