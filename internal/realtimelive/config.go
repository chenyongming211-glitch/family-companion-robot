// Package realtimelive preserves the standalone output-live protocol.
package realtimelive

import (
	"errors"
	"net/url"
	"os"
	"strings"
)

type Config struct {
	Key, URL, ASRURL, Voice, Model string
	Search                         *bool
	Tools                          []any
	Synthetic                      bool
	MaxSessions                    int
}

func LoadConfig(path string) (Config, error) {
	b, e := os.ReadFile(path)
	if e != nil {
		return Config{}, e
	}
	search := os.Getenv("OUTPUT_LIVE_ENABLE_SEARCH")
	if search == "" {
		search = "1"
	}
	return ParseConfig(string(b), search)
}

// ParseConfig reads data, never shell; ignores keys outside the baseline whitelist.
func ParseConfig(data, search string) (Config, error) {
	c := Config{Voice: "Tina"}
	if search != "0" && search != "1" {
		return c, errors.New("invalid_search_setting")
	}
	v := search == "1"
	c.Search = &v
	values := map[string]string{}
	for _, line := range strings.Split(data, "\n") {
		k, v, ok := strings.Cut(line, "=")
		if !ok || (k != "DASHSCOPE_API_KEY" && k != "QWEN_REALTIME_URL" && k != "QWEN_VOICE") {
			continue
		}
		v = strings.TrimSpace(v)
		if len(v) > 0 && (v[0] == '\'' || v[0] == '"') {
			q := v[0]
			if len(v) < 2 || v[len(v)-1] != q {
				return c, errors.New("protected_config_invalid")
			}
			v = v[1 : len(v)-1]
		}
		if strings.ContainsAny(v, "\x00\r\n`$\\ \t") {
			return c, errors.New("protected_config_invalid")
		}
		values[k] = v
	}
	c.Key = values["DASHSCOPE_API_KEY"]
	c.URL = values["QWEN_REALTIME_URL"]
	if v, ok := values["QWEN_VOICE"]; ok {
		c.Voice = v
	}
	u, e := url.Parse(c.URL)
	if e != nil || u.Scheme != "wss" || u.Hostname() == "" || u.User != nil || c.Key == "" {
		return c, errors.New("protected_config_invalid")
	}
	c.Model = u.Query().Get("model")
	if c.Model == "" {
		c.Model = "native-realtime"
	}
	u.Path = "/api-ws/v1/inference"
	u.RawQuery = ""
	u.Fragment = ""
	c.ASRURL = u.String()
	return c, nil
}
func (c Config) facts() map[string]any {
	m := map[string]any{"model": c.Model, "QWEN_REALTIME_URL": c.URL, "QWEN_VOICE": c.Voice}
	if c.Search != nil {
		m["enable_search"] = *c.Search
	}
	return m
}
func object(v any) map[string]any {
	m, _ := v.(map[string]any)
	if m == nil {
		return map[string]any{}
	}
	return m
}

// The server and each session own their optional mutable configuration values.
// Tools are used only as a presence flag; no code mutates or reads their content.
func (c Config) snapshot() Config {
	if c.Search != nil {
		value := *c.Search
		c.Search = &value
	}
	c.Tools = append([]any(nil), c.Tools...)
	return c
}
