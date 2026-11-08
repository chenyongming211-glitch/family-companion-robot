// Package deviceprofile stores noncredential device presentation settings. It
// never enrolls a device or changes an authentication binding.
package deviceprofile

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"unicode"
	"unicode/utf8"
)

var (
	ErrName        = errors.New("device_profile_name_invalid")
	ErrDeviceID    = errors.New("device_profile_device_invalid")
	ErrInvalid     = errors.New("device_profiles_invalid")
	ErrUnavailable = errors.New("device_profiles_unavailable")
	ErrUnsafePath  = errors.New("device_profiles_path_unsafe")
	ErrNotFound    = errors.New("device_profile_not_configured")
)
var deviceIDPattern = regexp.MustCompile(`^dev_[a-f0-9]{32}$`)

const maxBytes = 1 << 20
const maxProfiles = 1024

type Profile struct {
	Name     string `json:"name"`
	Revision uint64 `json:"revision"`
}
type document struct {
	Version  int                `json:"schema_version"`
	Profiles map[string]Profile `json:"profiles"`
}
type Store struct {
	mu       sync.Mutex
	path     string
	profiles map[string]Profile
}

// NormalizeName applies the same contract to managed profiles and immutable
// product copies. Controls are rejected before trimming so a line break cannot
// be smuggled into a management request as surrounding whitespace.
func NormalizeName(name string) (string, error) {
	if !utf8.ValidString(name) {
		return "", ErrName
	}
	for _, r := range name {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			return "", ErrName
		}
	}
	name = strings.TrimSpace(name)
	n := utf8.RuneCountInString(name)
	if n < 1 || n > 12 {
		return "", ErrName
	}
	return name, nil
}

// Open returns an empty usable cache even if the initial document is invalid.
// Readers may recover on a later session; administrators must handle the error.
// An absent initial file is normal for a store that has never been configured.
func Open(path string) (*Store, error) {
	s := &Store{path: path, profiles: map[string]Profile{}}
	profiles, err := s.read()
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	s.profiles = profiles
	return s, nil
}

// Resolve refreshes only from a wholly valid document. Failed refreshes retain
// the last good cache. The error is a bounded, noncontent reason for logging.
func (s *Store) Resolve(deviceID, defaultName string) (Profile, error) {
	if !deviceIDPattern.MatchString(deviceID) {
		return Profile{Name: defaultName}, ErrDeviceID
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	profiles, err := s.read()
	if err == nil {
		s.profiles = profiles
	} else if errors.Is(err, os.ErrNotExist) {
		err = ErrUnavailable
	}
	if p, ok := s.profiles[deviceID]; ok {
		return p, err
	}
	if err == nil {
		err = ErrNotFound
	}
	return Profile{Name: defaultName}, err
}

// SetName serializes independent management processes with a stable lock file,
// rereads the latest valid document under that lock, and atomically replaces it.
func (s *Store) SetName(deviceID, name string) (Profile, error) {
	if !deviceIDPattern.MatchString(deviceID) {
		return Profile{}, ErrDeviceID
	}
	name, err := NormalizeName(name)
	if err != nil {
		return Profile{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err = s.checkPath(); err != nil {
		return Profile{}, err
	}
	lock, err := privateOpen(s.path+".lock", syscall.O_RDWR|syscall.O_CREAT)
	if err != nil {
		return Profile{}, err
	}
	defer lock.Close()
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return Profile{}, ErrUnavailable
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	// The lock inode must still be the one advertised by the lock pathname.
	info, err := os.Lstat(s.path + ".lock")
	opened, statErr := lock.Stat()
	if err != nil || statErr != nil || !os.SameFile(info, opened) {
		return Profile{}, ErrUnsafePath
	}
	profiles, err := s.read()
	if errors.Is(err, os.ErrNotExist) {
		profiles = map[string]Profile{}
	} else if err != nil {
		return Profile{}, err
	}
	if len(profiles) >= maxProfiles {
		if _, ok := profiles[deviceID]; !ok {
			return Profile{}, ErrInvalid
		}
	}
	old := profiles[deviceID]
	if old.Revision == math.MaxUint64 {
		return Profile{}, ErrInvalid
	}
	next := Profile{Name: name, Revision: old.Revision + 1}
	profiles[deviceID] = next
	raw, err := json.Marshal(document{Version: 1, Profiles: profiles})
	if err != nil || len(raw) > maxBytes {
		return Profile{}, ErrInvalid
	}
	f, err := os.CreateTemp(filepath.Dir(s.path), ".device-profiles-*")
	if err != nil {
		return Profile{}, ErrUnavailable
	}
	temp := f.Name()
	defer os.Remove(temp)
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(append(raw, '\n'))
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil || closeErr != nil {
		return Profile{}, ErrUnavailable
	}
	if err = s.checkPath(); err != nil {
		return Profile{}, err
	}
	if err = os.Rename(temp, s.path); err != nil {
		return Profile{}, ErrUnavailable
	}
	s.profiles = profiles
	dir, err := os.Open(filepath.Dir(s.path))
	if err != nil {
		return Profile{}, ErrUnavailable
	}
	defer dir.Close()
	if err = dir.Sync(); err != nil {
		return Profile{}, ErrUnavailable
	}
	return next, nil
}

// Reject aliases in every existing parent, unsafe direct parents, and
// nonregular/public target files. The trusted parent prevents other users from
// replacing the destination or lock while a management operation is running.
func (s *Store) checkPath() error {
	for _, part := range strings.Split(filepath.ToSlash(s.path), "/") {
		if part == ".." {
			return ErrUnsafePath
		}
	}
	if s.path == "" || filepath.Base(s.path) == "." || filepath.Base(s.path) == string(filepath.Separator) {
		return ErrUnsafePath
	}
	absolute, err := filepath.Abs(s.path)
	if err != nil {
		return ErrUnsafePath
	}
	parent := filepath.Dir(absolute)
	for current := parent; ; current = filepath.Dir(current) {
		st, err := os.Lstat(current)
		if err != nil {
			return ErrUnsafePath
		}
		if !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
			return ErrUnsafePath
		}
		if current == parent && st.Mode().Perm()&0022 != 0 {
			return ErrUnsafePath
		}
		if filepath.Dir(current) == current {
			break
		}
	}
	st, err := os.Lstat(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || !st.Mode().IsRegular() || st.Mode().Perm()&0077 != 0 {
		return ErrUnsafePath
	}
	return nil
}

func privateOpen(path string, flags int) (*os.File, error) {
	fd, err := syscall.Open(path, flags|syscall.O_NOFOLLOW|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0600)
	if err != nil {
		return nil, ErrUnsafePath
	}
	f := os.NewFile(uintptr(fd), path)
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() || st.Mode().Perm()&0077 != 0 {
		f.Close()
		return nil, ErrUnsafePath
	}
	return f, nil
}

func (s *Store) read() (map[string]Profile, error) {
	if err := s.checkPath(); err != nil {
		return nil, err
	}
	// Preserve absence as a distinct condition for the first management write.
	if _, err := os.Lstat(s.path); errors.Is(err, os.ErrNotExist) {
		return nil, os.ErrNotExist
	}
	f, err := privateOpen(s.path, syscall.O_RDONLY)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || st.Size() > maxBytes {
		return nil, ErrInvalid
	}
	raw, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if err != nil {
		return nil, ErrUnavailable
	}
	if len(raw) > maxBytes || !utf8.Valid(raw) {
		return nil, ErrInvalid
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var d document
	if dec.Decode(&d) != nil || d.Version != 1 || d.Profiles == nil || len(d.Profiles) > maxProfiles {
		return nil, ErrInvalid
	}
	var trailing any
	if dec.Decode(&trailing) != io.EOF || uniqueKeys(json.NewDecoder(bytes.NewReader(raw)), 0) != nil {
		return nil, ErrInvalid
	}
	for id, p := range d.Profiles {
		name, err := NormalizeName(p.Name)
		if !deviceIDPattern.MatchString(id) || err != nil || name != p.Name || p.Revision < 1 {
			return nil, ErrInvalid
		}
	}
	return d.Profiles, nil
}

func uniqueKeys(dec *json.Decoder, depth int) error {
	if depth > 8 {
		return ErrInvalid
	}
	token, err := dec.Token()
	if err != nil {
		return ErrInvalid
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		seen := map[string]bool{}
		for dec.More() {
			key, err := dec.Token()
			name, ok := key.(string)
			if err != nil || !ok || seen[name] {
				return ErrInvalid
			}
			seen[name] = true
			if err := uniqueKeys(dec, depth+1); err != nil {
				return err
			}
		}
	case '[':
		for dec.More() {
			if err := uniqueKeys(dec, depth+1); err != nil {
				return err
			}
		}
	default:
		return ErrInvalid
	}
	_, err = dec.Token()
	return err
}
