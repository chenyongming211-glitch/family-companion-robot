package realtimelive

import "strings"

// searchDecision reports the per-turn search setting and why it was chosen.
// When search is enabled in the service configuration, whether a specific turn
// actually performs a web lookup is decided by the model's built-in search
// agent rather than a static server-side keyword list.
func searchDecision(user string, configured *bool) (*bool, string) {
	if configured == nil {
		return nil, "unset"
	}
	if !*configured {
		return configured, "disabled_by_config"
	}
	if strings.TrimSpace(user) == "" {
		return configured, "no_transcript"
	}
	return configured, "default_on"
}
