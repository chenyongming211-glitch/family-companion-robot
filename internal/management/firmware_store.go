package management

import (
	"bytes"
	"crypto/md5"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
)

const firmwareLayout = "waveshare-s3-16m-v1"
const firmwareSlotBytes = 3604480
const firmwareMaxApp = firmwareSlotBytes - 65536
const firmwareMaxReleases = 8
const firmwareMaxBytes = 32 << 20

var firmwareReleaseID = regexp.MustCompile(`^fw_[0-9a-f]{64}$`)
var firmwareJobID = regexp.MustCompile(`^ota_[0-9a-f]{32}$`)
var firmwareCommit = regexp.MustCompile(`^[0-9a-f]{40}$`)
var firmwareVersion = regexp.MustCompile(`^[0-9]{1,5}\.[0-9]{1,5}\.[0-9]{1,5}$`)

// FirmwareManifest is the strict publication input. All identity fields are
// checked against app.bin/partition-table.bin, never trusted as user assertions.
type FirmwareManifest struct {
	SchemaVersion           int      `json:"schemaVersion"`
	Board                   string   `json:"board"`
	Chip                    string   `json:"chip"`
	Project                 string   `json:"project"`
	Layout                  string   `json:"layout"`
	SlotBytes               int      `json:"slotBytes"`
	Version                 string   `json:"version"`
	SourceCommit            string   `json:"sourceCommit"`
	Size                    int64    `json:"size"`
	SHA256                  string   `json:"sha256"`
	ElfSHA256               string   `json:"elfSha256"`
	PartitionSHA256         string   `json:"partitionSha256"`
	CompatibleFromElfSHA256 []string `json:"compatibleFromElfSha256"`
}
type firmwarePartition struct {
	Label                       string
	Type, Subtype, Offset, Size int
}

var firmwarePartitions = []firmwarePartition{
	{"nvs", 1, 2, 0x9000, 0x6000}, {"otadata", 1, 0, 0xf000, 0x2000}, {"phy_init", 1, 1, 0x11000, 0x1000},
	{"ota_0", 0, 0x10, 0x20000, 0x370000}, {"ota_1", 0, 0x11, 0x390000, 0x370000}, {"assets", 1, 0x82, 0x700000, 0x600000}, {"storage", 1, 0x82, 0xd00000, 0x300000},
}

func firmwareStrict(raw []byte, v any) error {
	if len(raw) == 0 || uniqueConfigKeys(json.NewDecoder(bytes.NewReader(raw)), 0) != nil {
		return ErrInvalid
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(v) != nil {
		return ErrInvalid
	}
	var tail any
	if d.Decode(&tail) != io.EOF {
		return ErrInvalid
	}
	return nil
}
func firmwareManifestValid(m FirmwareManifest) bool {
	if m.SchemaVersion != 1 || m.Board != "waveshare-s3" || m.Chip != "esp32s3" || m.Project != "xiaozhi" || m.Layout != firmwareLayout || m.SlotBytes != firmwareSlotBytes || !firmwareVersion.MatchString(m.Version) || !firmwareCommit.MatchString(m.SourceCommit) || m.Size < 288 || m.Size > firmwareMaxApp || !digest.MatchString(m.SHA256) || !digest.MatchString(m.ElfSHA256) || !digest.MatchString(m.PartitionSHA256) || m.CompatibleFromElfSHA256 == nil || len(m.CompatibleFromElfSHA256) > 16 {
		return false
	}
	seen := map[string]bool{}
	for _, h := range m.CompatibleFromElfSHA256 {
		if !digest.MatchString(h) || seen[h] || h == m.ElfSHA256 {
			return false
		}
		seen[h] = true
	}
	return true
}
func firmwareHash(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func firmwareCString(b []byte) string {
	at := bytes.IndexByte(b, 0)
	if at < 0 {
		return ""
	}
	for _, v := range b[at:] {
		if v != 0 {
			return ""
		}
	}
	return string(b[:at])
}
func firmwareImageValid(app []byte, m FirmwareManifest) bool {
	if int64(len(app)) != m.Size || firmwareHash(app) != m.SHA256 || len(app) < 288 || app[0] != 0xe9 || app[1] < 1 || app[1] > 16 || binary.LittleEndian.Uint16(app[12:]) != 9 || binary.LittleEndian.Uint32(app[32:]) != 0xabcd5432 || firmwareCString(app[48:80]) != m.Version || firmwareCString(app[80:112]) != "xiaozhi" || hex.EncodeToString(app[176:208]) != m.ElfSHA256 {
		return false
	}
	off := 24
	checksum := byte(0xef)
	for i := 0; i < int(app[1]); i++ {
		if off+8 > len(app) {
			return false
		}
		n := int(binary.LittleEndian.Uint32(app[off+4:]))
		off += 8
		if n <= 0 || n > len(app)-off || i == 0 && n < 256 {
			return false
		}
		for _, v := range app[off : off+n] {
			checksum ^= v
		}
		off += n
	}
	end := (off + 16) &^ 15
	if end > len(app) || app[end-1] != checksum {
		return false
	}
	if app[23] == 1 {
		return len(app) == end+32 && firmwareHash(app[:end]) == hex.EncodeToString(app[end:])
	}
	return app[23] == 0 && len(app) == end
}
func firmwareTableValid(table []byte) bool {
	if len(table) < 256 || len(table) > 4096 || len(table)%32 != 0 {
		return false
	}
	for i, p := range firmwarePartitions {
		q := table[i*32 : (i+1)*32]
		if binary.LittleEndian.Uint16(q) != 0x50aa || int(q[2]) != p.Type || int(q[3]) != p.Subtype || int(binary.LittleEndian.Uint32(q[4:])) != p.Offset || int(binary.LittleEndian.Uint32(q[8:])) != p.Size || firmwareCString(q[12:28]) != p.Label || binary.LittleEndian.Uint32(q[28:]) != 0 {
			return false
		}
	}
	pos := len(firmwarePartitions) * 32
	q := table[pos : pos+32]
	h := md5.Sum(table[:pos])
	return q[0] == 0xeb && q[1] == 0xeb && bytes.Equal(q[2:16], bytes.Repeat([]byte{255}, 14)) && bytes.Equal(q[16:], h[:]) && bytes.Equal(table[pos+32:], bytes.Repeat([]byte{255}, len(table)-pos-32))
}

// Reject symlinks in every supplied path, not only the final component.
func firmwareNoSymlinks(p string) error {
	abs, e := filepath.Abs(p)
	if e != nil {
		return ErrInvalid
	}
	cur := string(filepath.Separator)
	for _, part := range strings.Split(strings.TrimPrefix(abs, string(filepath.Separator)), string(filepath.Separator)) {
		cur = filepath.Join(cur, part)
		st, e := os.Lstat(cur)
		if e != nil {
			return e
		}
		if st.Mode()&os.ModeSymlink != 0 {
			return ErrInvalid
		}
	}
	return nil
}
func firmwareReadPath(p string, max int64) ([]byte, error) {
	if firmwareNoSymlinks(p) != nil {
		return nil, ErrInvalid
	}
	f, e := os.OpenFile(p, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if e != nil {
		return nil, ErrSource
	}
	defer f.Close()
	st, e := f.Stat()
	if e != nil || !st.Mode().IsRegular() || st.Size() > max {
		return nil, ErrInvalid
	}
	b, e := io.ReadAll(io.LimitReader(f, max+1))
	if e != nil || int64(len(b)) > max {
		return nil, ErrInvalid
	}
	return b, nil
}

type firmwareStore struct {
	root *os.Root
	lock *os.File
}

func firmwareOpen(p string) (*firmwareStore, error) {
	if p == "" {
		return nil, ErrSource
	}
	if _, e := os.Lstat(p); os.IsNotExist(e) {
		if firmwareNoSymlinks(filepath.Dir(p)) != nil {
			return nil, ErrInvalid
		}
		if e = os.Mkdir(p, 0700); e != nil && !os.IsExist(e) {
			return nil, ErrSource
		}
	}
	if firmwareNoSymlinks(p) != nil {
		return nil, ErrSource
	}
	st, e := os.Lstat(p)
	if e != nil || !st.IsDir() || st.Mode().Perm() != 0700 {
		return nil, ErrSource
	}
	root, e := os.OpenRoot(p)
	if e != nil {
		return nil, ErrSource
	}
	store := &firmwareStore{root: root}
	ok := false
	defer func() {
		if !ok {
			store.close()
		}
	}()
	lock, e := root.OpenFile(".lock", os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0600)
	if e != nil {
		return nil, ErrSource
	}
	store.lock = lock
	st, e = lock.Stat()
	if e != nil || !st.Mode().IsRegular() || st.Mode().Perm() != 0600 || syscall.Flock(int(lock.Fd()), syscall.LOCK_EX) != nil {
		return nil, ErrSource
	}
	for _, name := range []string{"releases", "devices"} {
		if e = root.Mkdir(name, 0700); e != nil && !os.IsExist(e) {
			return nil, ErrSource
		}
		st, e = root.Lstat(name)
		if e != nil || !st.IsDir() || st.Mode().Perm() != 0700 {
			return nil, ErrSource
		}
	}
	ok = true
	return store, nil
}
func (f *firmwareStore) close() {
	if f.lock != nil {
		_ = syscall.Flock(int(f.lock.Fd()), syscall.LOCK_UN)
		f.lock.Close()
		f.lock = nil
	}
	if f.root != nil {
		f.root.Close()
		f.root = nil
	}
}
func (f *firmwareStore) read(name string, max int64) ([]byte, error) {
	parts := strings.Split(name, "/")
	for i := 1; i < len(parts); i++ {
		st, e := f.root.Lstat(strings.Join(parts[:i], "/"))
		if e != nil || !st.IsDir() || st.Mode().Perm() != 0700 {
			return nil, ErrSource
		}
	}
	file, e := f.root.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if e != nil {
		if os.IsNotExist(e) {
			return nil, ErrNotFound
		}
		return nil, ErrSource
	}
	defer file.Close()
	st, e := file.Stat()
	if e != nil || !st.Mode().IsRegular() || st.Mode().Perm() != 0600 || st.Size() > max {
		return nil, ErrSource
	}
	b, e := io.ReadAll(io.LimitReader(file, max+1))
	if e != nil || int64(len(b)) > max {
		return nil, ErrSource
	}
	return b, nil
}
func (f *firmwareStore) syncDir(name string) error {
	d, e := f.root.Open(name)
	if e != nil {
		return ErrSource
	}
	defer d.Close()
	if d.Sync() != nil {
		return ErrSource
	}
	return nil
}
func (f *firmwareStore) writeNew(name string, raw []byte) error {
	file, e := f.root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY|syscall.O_NOFOLLOW, 0600)
	if e != nil {
		return ErrSource
	}
	defer file.Close()
	if _, e = file.Write(raw); e != nil {
		return ErrSource
	}
	if file.Sync() != nil {
		return ErrSource
	}
	return nil
}
func (f *firmwareStore) atomic(name string, v any) error {
	raw, e := json.Marshal(v)
	if e != nil || len(raw) > 256<<10 {
		return ErrSource
	}
	if st, e := f.root.Lstat(name); e == nil && (!st.Mode().IsRegular() || st.Mode().Perm() != 0600) {
		return ErrSource
	} else if e != nil && !os.IsNotExist(e) {
		return ErrSource
	}
	suffix, e := randomHex(8)
	if e != nil {
		return ErrSource
	}
	tmp := name + "." + suffix + ".tmp"
	defer f.root.Remove(tmp)
	if e = f.writeNew(tmp, raw); e != nil {
		return e
	}
	if e = f.root.Rename(tmp, name); e != nil {
		return ErrSource
	}
	return f.syncDir(filepath.Dir(name))
}
func (f *firmwareStore) release(id string) (FirmwareManifest, []byte, error) {
	var m FirmwareManifest
	if !firmwareReleaseID.MatchString(id) {
		return m, nil, ErrInvalid
	}
	raw, e := f.read("releases/"+id+"/manifest.json", 8192)
	if e != nil {
		return m, nil, e
	}
	if firmwareStrict(raw, &m) != nil || !firmwareManifestValid(m) || id != "fw_"+m.SHA256 {
		return m, nil, ErrSource
	}
	app, e := f.read("releases/"+id+"/app.bin", firmwareMaxApp)
	if e != nil || !firmwareImageValid(app, m) {
		return m, nil, ErrSource
	}
	return m, app, nil
}
func (f *firmwareStore) releases() ([]FirmwareManifest, error) {
	dir, e := f.root.Open("releases")
	if e != nil {
		return nil, ErrSource
	}
	defer dir.Close()
	names, e := dir.Readdirnames(firmwareMaxReleases + 1)
	if e != nil && !errors.Is(e, io.EOF) || len(names) > firmwareMaxReleases {
		return nil, ErrSource
	}
	sort.Strings(names)
	out := []FirmwareManifest{}
	var total int64
	for _, id := range names {
		m, _, e := f.release(id)
		if e != nil {
			return nil, ErrSource
		}
		total += m.Size
		if total > firmwareMaxBytes {
			return nil, ErrSource
		}
		out = append(out, m)
	}
	return out, nil
}

// PublishFirmware atomically seals one checked release. Existing release IDs
// are never overwritten, including by a repeated publication of identical bytes.
func PublishFirmware(root, manifest, app, partition string) (string, error) {
	raw, e := firmwareReadPath(manifest, 8192)
	if e != nil {
		return "", e
	}
	var m FirmwareManifest
	if firmwareStrict(raw, &m) != nil || !firmwareManifestValid(m) {
		return "", ErrInvalid
	}
	a, e := firmwareReadPath(app, firmwareMaxApp)
	if e != nil || !firmwareImageValid(a, m) {
		return "", ErrInvalid
	}
	p, e := firmwareReadPath(partition, 4096)
	if e != nil || firmwareHash(p) != m.PartitionSHA256 || !firmwareTableValid(p) {
		return "", ErrInvalid
	}
	f, e := firmwareOpen(root)
	if e != nil {
		return "", e
	}
	defer f.close()
	releases, e := f.releases()
	if e != nil {
		return "", e
	}
	var total int64
	for _, r := range releases {
		total += r.Size
	}
	if len(releases) >= firmwareMaxReleases || total+m.Size > firmwareMaxBytes {
		return "", errLimit
	}
	id := "fw_" + m.SHA256
	if _, e = f.root.Lstat("releases/" + id); !os.IsNotExist(e) {
		return "", errConflict
	}
	suffix, e := randomHex(8)
	if e != nil {
		return "", e
	}
	tmp := ".publish_" + suffix
	if e = f.root.Mkdir(tmp, 0700); e != nil {
		return "", ErrSource
	}
	defer func() { f.root.Remove(tmp + "/manifest.json"); f.root.Remove(tmp + "/app.bin"); f.root.Remove(tmp) }()
	canonical, _ := json.Marshal(m)
	if f.writeNew(tmp+"/manifest.json", canonical) != nil || f.writeNew(tmp+"/app.bin", a) != nil || f.syncDir(tmp) != nil {
		return "", ErrSource
	}
	if f.root.Rename(tmp, "releases/"+id) != nil || f.syncDir("releases") != nil {
		return "", ErrSource
	}
	return id, nil
}
