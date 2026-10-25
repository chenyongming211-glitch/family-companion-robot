package realtimelive

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
)

func decodeObject(b []byte) (map[string]any, error) {
	var m map[string]any
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	if e := d.Decode(&m); e != nil {
		return nil, e
	}
	if m == nil {
		return nil, errors.New("invalid_json_object")
	}
	var extra any
	if e := d.Decode(&extra); e != io.EOF {
		return nil, errors.New("trailing_json")
	}
	return m, nil
}
