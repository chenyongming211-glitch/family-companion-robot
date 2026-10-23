// Package recordstore persists bounded per-device text snapshots. A single worker
// owns the directory; a Save receipt, rather than admission to its queue, reports
// whether the write and its directory entry were synced successfully.
package recordstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	queueSize   = 32
	maxFileSize = 1 << 20
	maxDevices  = 8
)

var (
	ErrNotFound       = errors.New("snapshot not found")
	ErrBusy           = errors.New("snapshot queue full")
	ErrClosed         = errors.New("snapshot store closed")
	ErrInvalid        = errors.New("invalid snapshot or device")
	ErrTooLarge       = errors.New("snapshot exceeds 1 MiB")
	ErrTooManyDevices = errors.New("snapshot device limit reached")
	ErrCorrupt        = errors.New("corrupt snapshot")
	ErrLocked         = errors.New("snapshot directory already locked")
)

type Options struct {
	Directory string
	Retention time.Duration
}
type Snapshot struct {
	Version   int             `json:"version"`
	UpdatedAt time.Time       `json:"updated_at"`
	Data      json.RawMessage `json:"data"`
}

// Store admits at most 32 waiting operations in addition to the running operation.
// Save performs bounded validation/copying and never waits for disk or queue space.
// Load and Clear share its FIFO. Cancellation before execution skips their work;
// cancellation during disk I/O may return before that operation finishes.
type Store struct {
	mu        sync.Mutex
	closed    bool
	jobs      chan func()
	done      chan struct{}
	directory string
	retention time.Duration
	lock      *os.File
	devices   map[string]time.Time // worker owned, keyed by SHA256
	closeErr  error                // published by closing done
}

func Open(opts Options) (*Store, error) {
	if opts.Directory == "" || opts.Retention <= 0 {
		return nil, ErrInvalid
	}
	s := &Store{jobs: make(chan func(), queueSize), done: make(chan struct{}), directory: opts.Directory, retention: opts.Retention, devices: make(map[string]time.Time)}
	ready := make(chan error, 1)
	go func() {
		err := s.initialize()
		ready <- err
		if err == nil {
			s.work()
		}
		if s.lock != nil {
			s.closeErr = s.lock.Close()
		}
		close(s.done)
	}()
	if err := <-ready; err != nil {
		<-s.done
		return nil, err
	}
	return s, nil
}

func (s *Store) enqueue(job func()) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrClosed
	}
	select {
	case s.jobs <- job:
		return nil
	default:
		return ErrBusy
	}
}

func deviceKey(device string) (string, error) {
	if len(device) > 1024 || strings.TrimSpace(device) == "" {
		return "", ErrInvalid
	}
	sum := sha256.Sum256([]byte(device))
	return hex.EncodeToString(sum[:]), nil
}
func validSnapshot(v Snapshot) bool {
	return v.Version == 1 && !v.UpdatedAt.IsZero() && !v.UpdatedAt.After(time.Now()) && len(v.Data) > 0 && strings.TrimSpace(string(v.Data)) != "null" && json.Valid(v.Data)
}

func (s *Store) Save(device string, snapshot Snapshot) (<-chan error, error) {
	key, err := deviceKey(device)
	if err != nil {
		return nil, err
	}
	if len(snapshot.Data) > maxFileSize {
		return nil, ErrTooLarge
	}
	if !validSnapshot(snapshot) {
		return nil, ErrInvalid
	}
	// Marshal makes an owned copy and accounts for envelope overhead and escaping.
	data, err := json.Marshal(snapshot)
	if err != nil {
		return nil, ErrInvalid
	}
	if len(data) > maxFileSize {
		return nil, ErrTooLarge
	}
	receipt := make(chan error, 1)
	updated := snapshot.UpdatedAt
	err = s.enqueue(func() { receipt <- s.write(key, updated, data); close(receipt) })
	if err != nil {
		return nil, err
	}
	return receipt, nil
}

func (s *Store) Load(ctx context.Context, device string) (Snapshot, error) {
	key, err := deviceKey(device)
	if err != nil {
		return Snapshot{}, err
	}
	if err = ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	type result struct {
		snapshot Snapshot
		err      error
	}
	reply := make(chan result, 1)
	err = s.enqueue(func() {
		if err := ctx.Err(); err != nil {
			reply <- result{err: err}
			return
		}
		v, e := s.read(key)
		reply <- result{v, e}
	})
	if err != nil {
		return Snapshot{}, err
	}
	select {
	case v := <-reply:
		return v.snapshot, v.err
	case <-ctx.Done():
		return Snapshot{}, ctx.Err()
	}
}

func (s *Store) Clear(ctx context.Context, device string) error {
	key, err := deviceKey(device)
	if err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	reply := make(chan error, 1)
	err = s.enqueue(func() {
		if err := ctx.Err(); err != nil {
			reply <- err
			return
		}
		reply <- s.remove(key)
	})
	if err != nil {
		return err
	}
	select {
	case err := <-reply:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Close stops admission immediately, drains accepted operations in FIFO order,
// and releases the lock. A timeout does not discard work or release the lock
// early; later Close calls can wait for the same worker to finish.
func (s *Store) Close(ctx context.Context) error {
	s.mu.Lock()
	if !s.closed {
		s.closed = true
		close(s.jobs)
	}
	s.mu.Unlock()
	select {
	case <-s.done:
		return s.closeErr
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Store) path(key string) string { return filepath.Join(s.directory, key+".json") }
func (s *Store) initialize() error {
	if err := os.MkdirAll(s.directory, 0700); err != nil {
		return fmt.Errorf("create snapshot directory: %w", err)
	}
	info, err := os.Lstat(s.directory)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return ErrInvalid
	}
	// O_NOFOLLOW protects the lock from a preexisting symlink; flock locks its
	// inode, so path aliases still refer to the same exclusive lock.
	lock, err := openStoreFile(filepath.Join(s.directory, ".lock"), syscall.O_CREAT|syscall.O_RDWR, 0600)
	if err != nil {
		return fmt.Errorf("open snapshot lock: %w", err)
	}
	s.lock = lock
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return fmt.Errorf("%w: %v", ErrLocked, err)
	}
	if err = os.Chmod(s.directory, 0700); err != nil {
		return err
	}
	if err = s.lock.Chmod(0600); err != nil {
		return err
	}
	entries, err := os.ReadDir(s.directory)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".snapshot-") {
			if err = os.Remove(filepath.Join(s.directory, name)); err != nil {
				return err
			}
			continue
		}
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		key := strings.TrimSuffix(name, ".json")
		decoded, e := hex.DecodeString(key)
		if e != nil || len(decoded) != sha256.Size || key != strings.ToLower(key) {
			return ErrCorrupt
		}
		v, e := s.readFile(key)
		if e != nil {
			return e
		}
		if s.expired(v.UpdatedAt) {
			if e = s.remove(key); e != nil {
				return e
			}
			continue
		}
		if e = os.Chmod(s.path(key), 0600); e != nil {
			return e
		}
		s.devices[key] = v.UpdatedAt
	}
	if len(s.devices) > maxDevices {
		return ErrTooManyDevices
	}
	return s.syncDirectory()
}
func (s *Store) expired(t time.Time) bool { return !t.After(time.Now().Add(-s.retention)) }
func (s *Store) cleanupExpired() error {
	for key, t := range s.devices {
		if s.expired(t) {
			if err := s.remove(key); err != nil {
				return err
			}
		}
	}
	return nil
}

func openStoreFile(path string, flags int, mode uint32) (*os.File, error) {
	// Set close-on-exec atomically with creation: a later CloseOnExec call
	// leaves a window in which another goroutine can fork an inheriting child.
	fd, err := syscall.Open(path, flags|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, mode)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), path), nil
}

func (s *Store) readFile(key string) (Snapshot, error) {
	path := s.path(key)
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return Snapshot{}, ErrNotFound
	}
	if err != nil {
		return Snapshot{}, err
	}
	if !info.Mode().IsRegular() {
		return Snapshot{}, ErrCorrupt
	}
	if info.Size() > maxFileSize {
		return Snapshot{}, ErrTooLarge
	}
	f, err := openStoreFile(path, syscall.O_RDONLY, 0)
	if err != nil {
		return Snapshot{}, err
	}
	b, readErr := io.ReadAll(io.LimitReader(f, maxFileSize+1))
	closeErr := f.Close()
	if readErr != nil {
		return Snapshot{}, readErr
	}
	if closeErr != nil {
		return Snapshot{}, closeErr
	}
	if len(b) > maxFileSize {
		return Snapshot{}, ErrTooLarge
	}
	var v Snapshot
	if json.Unmarshal(b, &v) != nil || !validSnapshot(v) {
		return Snapshot{}, ErrCorrupt
	}
	return v, nil
}
func (s *Store) read(key string) (Snapshot, error) {
	if err := s.cleanupExpired(); err != nil {
		return Snapshot{}, err
	}
	v, err := s.readFile(key)
	if err != nil {
		return Snapshot{}, err
	}
	if s.expired(v.UpdatedAt) {
		if err = s.remove(key); err != nil {
			return Snapshot{}, err
		}
		return Snapshot{}, ErrNotFound
	}
	return v, nil
}
func (s *Store) remove(key string) error {
	err := os.Remove(s.path(key))
	if os.IsNotExist(err) {
		delete(s.devices, key)
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	delete(s.devices, key)
	return s.syncDirectory()
}
func (s *Store) write(key string, updated time.Time, data []byte) error {
	if err := s.cleanupExpired(); err != nil {
		return err
	}
	if s.expired(updated) {
		if _, ok := s.devices[key]; ok {
			return s.remove(key)
		}
		return nil
	}
	if _, ok := s.devices[key]; !ok && len(s.devices) >= maxDevices {
		return ErrTooManyDevices
	}
	f, err := os.CreateTemp(s.directory, ".snapshot-")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if err = f.Chmod(0600); err != nil {
		f.Close()
		return err
	}
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = os.Rename(name, s.path(key)); err != nil {
		return err
	}
	// The rename has happened even if syncing the directory fails. Account for
	// the file while returning the failure truthfully through the receipt.
	s.devices[key] = updated
	return s.syncDirectory()
}
func (s *Store) syncDirectory() error {
	dir, err := os.Open(s.directory)
	if err != nil {
		return err
	}
	syncErr := dir.Sync()
	closeErr := dir.Close()
	if syncErr != nil {
		return syncErr
	}
	return closeErr
}

// One worker owns both FIFO operations and maintenance. The ticker is bounded
// even for nanosecond retention; it cannot spin or create concurrent disk I/O.
func (s *Store) work() {
	cadence := s.retention / 4
	if cadence < 25*time.Millisecond {
		cadence = 25 * time.Millisecond
	}
	if cadence > time.Minute {
		cadence = time.Minute
	}
	ticker := time.NewTicker(cadence)
	defer ticker.Stop()
	for {
		select {
		case job, ok := <-s.jobs:
			if !ok {
				return
			}
			job()
		case <-ticker.C:
			if err := s.cleanupExpired(); err != nil {
				slog.Error("record_retention_cleanup_failed")
			}
		}
	}
}
