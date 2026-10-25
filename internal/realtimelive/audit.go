package realtimelive

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"familyrobot/internal/voiceidentity"
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
	return voiceidentity.WritePrivateJSON(path, v)
}

type intent struct {
	path  string
	data  map[string]any
	start time.Time
	owner Ownership
}

func newIntent(dir, stage string, synthetic bool) (*intent, error) {
	i := &intent{filepath.Join(dir, stage+"-"+randomID()+".json"), map[string]any{"stage": stage, "state": "intent_saved", "synthetic_only": synthetic}, time.Now(), Ownership{}}
	info, e := ReadSessionInfo(dir)
	if e != nil && !os.IsNotExist(e) {
		return nil, e
	}
	i.owner = info.Ownership
	i.owner.apply(i.data)
	return i, i.update(nil)
}
func (i *intent) update(fields map[string]any) error {
	for k, v := range fields {
		i.data[k] = v
	}
	i.owner.apply(i.data)
	i.data["elapsed_ms"] = time.Since(i.start).Milliseconds()
	return privateJSON(i.path, i.data)
}

type recorder struct {
	mu    sync.Mutex
	dir   string
	owner Ownership
}

func (r *recorder) add(event string, fields map[string]any) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	m := map[string]any{"event": event, "at": float64(time.Now().UnixNano()) / 1e9}
	for k, v := range fields {
		m[k] = v
	}
	r.owner.apply(m)
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
