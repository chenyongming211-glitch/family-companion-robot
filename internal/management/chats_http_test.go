package management

import (
	"errors"
	"strings"
	"testing"
)

func TestChatsHTTPAudit(t *testing.T) {
	s, _ := httpFixture(t)
	r, i := chatFixture(t)
	chatWrite(t, r, i, "source", map[string]any{"event": "user_text", "id": 1, "text": "合成问题"})
	s.reader.Root = r.Root
	for _, scope := range []string{"family", "admin"} {
		c := login(t, s, scope, "alice")
		w := request(s, "GET", "/api/"+scope+"/chats", "", c)
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body)
		}
		if strings.Contains(w.Body.String(), "合成问题") {
			t.Fatal("list body")
		}
	}
	c := login(t, s, "family", "alice")
	s.auditWrite = func(string, auditEntry) error { return errors.New("failure") }
	if w := request(s, "GET", "/api/family/chats", "", c); w.Code != 503 {
		t.Fatal(w.Code)
	}
}

func TestChatsHTTPQueriesDetailAndPermissions(t *testing.T) {
	s, _ := httpFixture(t)
	r, i := chatFixture(t)
	chatWrite(t, r, i, "source", map[string]any{"event": "user_text", "id": 1, "text": "合成问题"})
	s.reader.Root = r.Root
	p, e := r.ListChats("alice", "family", ConversationFilter{})
	if e != nil {
		t.Fatal(e)
	}
	id := p.Items[0].ID
	for _, scope := range []string{"family", "admin"} {
		c := login(t, s, scope, "alice")
		base := "/api/" + scope + "/chats"
		for _, tc := range []struct {
			suffix string
			status int
		}{{"", 200}, {"?limit=1&offset=0", 200}, {"?device=", 400}, {"?device=x", 400}, {"?offset=-1", 400}, {"?limit=101", 400}, {"?device=" + i.DeviceID + "&device=" + i.DeviceID, 400}, {"/bad", 400}, {"/" + id, 200}, {"/" + id + "?limit=1", 400}, {"?device=" + r.Config.Devices[1].ID, 403}} {
			w := request(s, "GET", base+tc.suffix, "", c)
			if w.Code != tc.status {
				t.Fatal(tc, w.Code, w.Body)
			}
		}
	}
	for _, user := range []string{"metadata", "observer"} {
		c := login(t, s, "family", user)
		for _, suffix := range []string{"", "/" + id} {
			w := request(s, "GET", "/api/family/chats"+suffix, "", c)
			if w.Code != 403 {
				t.Fatal(user, w.Code)
			}
		}
	}
	c := login(t, s, "family", "alice")
	actions := []string{}
	s.auditWrite = func(_ string, e auditEntry) error { actions = append(actions, e.Action); return nil }
	for _, suffix := range []string{"", "/" + id} {
		request(s, "GET", "/api/family/chats"+suffix, "", c)
	}
	if len(actions) != 2 || actions[0] != "list_chats" || actions[1] != "read_chat" {
		t.Fatal(actions)
	}
	s.auditWrite = func(string, auditEntry) error { return errors.New("synthetic") }
	w := request(s, "GET", "/api/family/chats/"+id, "", c)
	if w.Code != 503 || strings.Contains(w.Body.String(), "合成问题") {
		t.Fatal(w.Code, w.Body)
	}
}
