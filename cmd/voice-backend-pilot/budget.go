package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"math"
	"os"
	"strings"
	"syscall"
	"time"
)

// lockedLedger uses the existing append/fsync/flock convention. Reservations
// are never refunded, even after a successful or unknown provider outcome.
func lockedLedger(path string, create bool, update func([]map[string]any) (map[string]any, error)) error {
	flags := syscall.O_RDWR | syscall.O_APPEND | syscall.O_NOFOLLOW
	if create {
		flags |= syscall.O_CREAT
	}
	fd, err := syscall.Open(path, flags, 0600)
	if err != nil {
		return errors.New("ledger_unavailable")
	}
	f := os.NewFile(uintptr(fd), path)
	defer f.Close()
	if err = syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return errors.New("ledger_busy")
	}
	defer syscall.Flock(fd, syscall.LOCK_UN)
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() || st.Mode().Perm() != 0600 || st.Size() > 8<<20 {
		return errors.New("ledger_type_or_mode")
	}
	if st.Size() > 0 {
		var last [1]byte
		if _, err = f.ReadAt(last[:], st.Size()-1); err != nil || last[0] != '\n' {
			return errors.New("ledger_incomplete_tail")
		}
	}
	rows := []map[string]any{}
	s := bufio.NewScanner(f)
	s.Buffer(make([]byte, 4096), 1<<20)
	for s.Scan() {
		var row map[string]any
		if json.Unmarshal(s.Bytes(), &row) != nil || row == nil {
			return errors.New("ledger_corrupt")
		}
		rows = append(rows, row)
	}
	if s.Err() != nil {
		return errors.New("ledger_read_failed")
	}
	row, err := update(rows)
	if err != nil {
		return err
	}
	row["at"] = time.Now().UTC().Format(time.RFC3339Nano)
	if json.NewEncoder(f).Encode(row) != nil || f.Sync() != nil {
		return errors.New("ledger_intent_unknown")
	}
	return nil
}

func reserveBudget(path, stage string, yuan float64, fen int) error {
	if yuan <= 0 || math.IsNaN(yuan) || math.IsInf(yuan, 0) || fen < 1 || fen > 100 {
		return errors.New("invalid_reservation")
	}
	units := math.Ceil(yuan * 1000000)
	return lockedLedger(path, true, func(rows []map[string]any) (map[string]any, error) {
		used := 0.0
		pending := ""
		for _, r := range rows {
			if r["event"] == "intent" {
				n, ok := r["reserved_microyuan"].(float64)
				stageName, valid := r["stage"].(string)
				if !ok || n <= 0 || math.Trunc(n) != n || !valid || stageName == "" || pending != "" {
					return nil, errors.New("budget_corrupt")
				}
				used += n
				pending = stageName
			} else if r["event"] == "result" {
				unknown, ok := r["unknown"].(bool)
				if pending == "" || r["stage"] != pending || !ok {
					return nil, errors.New("budget_corrupt")
				}
				if unknown {
					return nil, errors.New("previous_api_outcome_unknown")
				}
				pending = ""
			} else {
				return nil, errors.New("budget_corrupt")
			}
		}
		if pending != "" {
			return nil, errors.New("previous_api_unfinished")
		}
		if used+units > float64(fen)*10000 {
			return nil, errors.New("api_budget_exhausted")
		}
		return map[string]any{"event": "intent", "stage": stage, "reserved_microyuan": units, "state": "outcome_unknown"}, nil
	})
}

func recordOutcome(path, stage string, usage map[string]any, unknown bool) error {
	return lockedLedger(path, false, func(rows []map[string]any) (map[string]any, error) {
		if len(rows) == 0 || rows[len(rows)-1]["event"] != "intent" || rows[len(rows)-1]["stage"] != stage {
			return nil, errors.New("budget_result_identity")
		}
		return map[string]any{"event": "result", "stage": stage, "usage": usage, "unknown": unknown || len(usage) == 0}, nil
	})
}

func reserveGlobal(path, backend, attempt string) error {
	if !validAttempt(attempt) {
		return errors.New("invalid_attempt")
	}
	stage := "primary"
	limit, ownLimit := 48, 1
	if backend == "tts" {
		stage, limit, ownLimit = "protocol", 8, 2
	}
	return lockedLedger(path, false, func(rows []map[string]any) (map[string]any, error) {
		count, own := 0, 0
		for _, r := range rows {
			if r["event"] != "intent" {
				continue
			}
			id, ok := r["attempt"].(string)
			if !ok || id == "" || (r["stage"] != "protocol" && r["stage"] != "primary") {
				return nil, errors.New("attempt_ledger_corrupt")
			}
			if id == attempt {
				return nil, errors.New("attempt_already_consumed")
			}
			if r["stage"] == stage {
				count++
				if strings.HasPrefix(id, "quiet-voice-20261114/") {
					if stage == "primary" && r["backend"] != "native" && r["backend"] != "cascade" {
						return nil, errors.New("attempt_backend_unknown")
					}
					if stage == "protocol" || r["backend"] == backend {
						own++
					}
				}
			}
		}
		if count >= limit || own >= ownLimit {
			return nil, errors.New("attempt_budget_exhausted")
		}
		return map[string]any{"event": "intent", "stage": stage, "backend": backend, "attempt": attempt, "state": "outcome_unknown"}, nil
	})
}
