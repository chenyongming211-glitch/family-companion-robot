package gateway

// Adapted in Go from Qwen-Live-Harness's bounded completed-output tombstones.
// See THIRD_PARTY_NOTICES.txt for the pinned source and Apache-2.0 license.
// This window belongs to one upstream generation; it is not a playback ACK.
const maxRetiredResponses = 256

type retiredResponses struct {
	ids   map[string]bool
	order []string
}

func (r *retiredResponses) contains(id string) bool { return r.ids[id] }

func (r *retiredResponses) add(id string) {
	if id == "" || r.contains(id) {
		return
	}
	if r.ids == nil {
		r.ids = make(map[string]bool)
	}
	r.ids[id] = true
	r.order = append(r.order, id)
	if len(r.order) > maxRetiredResponses {
		delete(r.ids, r.order[0])
		copy(r.order, r.order[1:])
		r.order = r.order[:maxRetiredResponses]
	}
}
