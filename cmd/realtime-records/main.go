// Server-side private record inspection. No network or model calls.
package main

import (
	"bufio"
	"encoding/json"
	"familyrobot/internal/realtimelive"
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

func stop(code string) { fmt.Fprintln(os.Stderr, code); os.Exit(1) }
func main() {
	root := flag.String("records", "/var/lib/family-robot-realtime-test/verification/output-live-go", "private record root")
	device := flag.String("device", "", "exact device ID filter")
	session := flag.String("session", "", "exact session ID or legacy record directory")
	events := flag.Bool("events", false, "explicitly output private events for one selected session")
	flag.Parse()
	if flag.NArg() != 0 || (*events && *session == "") {
		stop("select_one_session_for_events")
	}
	records, e := realtimelive.ListRecords(*root, *device, *session)
	if e != nil {
		stop("record_metadata_read_failed")
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)
	if !*events {
		for _, record := range records {
			if enc.Encode(record) != nil {
				stop("output_failed")
			}
		}
		return
	}
	if len(records) != 1 {
		stop("session_not_unique_or_not_found")
	}
	f, e := os.Open(filepath.Join(*root, records[0].RecordID, "events.jsonl"))
	if e != nil {
		stop("events_read_failed")
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 4096), 8<<20)
	for scanner.Scan() {
		var event json.RawMessage
		if json.Unmarshal(scanner.Bytes(), &event) != nil {
			stop("invalid_event_json")
		}
		if enc.Encode(event) != nil {
			stop("output_failed")
		}
	}
	if scanner.Err() != nil {
		stop("events_read_failed")
	}
}
