package realtimelive

import (
	"os"
	"path/filepath"
	"sort"
)

type RecordSummary struct {
	RecordID string `json:"record_id"`
	SessionInfo
}

// ListRecords reads metadata only. Missing legacy ownership is never inferred
// from directory timestamps, the current client or previous gateway snapshots.
func ListRecords(root, deviceID, sessionID string) ([]RecordSummary, error) {
	entries, e := os.ReadDir(root)
	if e != nil {
		return nil, e
	}
	records := []RecordSummary{}
	for _, entry := range entries {
		if !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		dir := filepath.Join(root, entry.Name())
		info, e := ReadSessionInfo(dir)
		if os.IsNotExist(e) {
			st, err := os.Lstat(filepath.Join(dir, "events.jsonl"))
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				return nil, err
			}
			if !st.Mode().IsRegular() {
				continue
			}
			info = SessionInfo{Ownership: Ownership{ClientType: "legacy_unattributed"}}
		} else if e != nil {
			return nil, e
		}
		if deviceID != "" && deviceID != info.DeviceID {
			continue
		}
		if sessionID != "" && sessionID != info.SessionID && sessionID != entry.Name() {
			continue
		}
		records = append(records, RecordSummary{entry.Name(), info})
	}
	sort.Slice(records, func(i, j int) bool { return records[i].RecordID < records[j].RecordID })
	return records, nil
}
