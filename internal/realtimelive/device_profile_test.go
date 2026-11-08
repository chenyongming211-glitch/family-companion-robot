package realtimelive

import (
	"bytes"
	"context"
	"familyrobot/internal/deviceprofile"
	"familyrobot/internal/realtimestate"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func namedServer(t *testing.T) (*Server, *deviceprofile.Store, string) {
	t.Helper()
	root := t.TempDir()
	path := filepath.Join(root, "profiles.json")
	store, err := deviceprofile.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	base, err := realtimestate.LoadProduct("../../config/product-identity.json")
	if err != nil {
		t.Fatal(err)
	}
	s := NewServer(Config{DeviceProfilesPath: path, MaxSessions: 2, MemoryEnabled: true}, base, root, "names")
	if s.InitializationError() != nil {
		t.Fatal(s.InitializationError())
	}
	t.Cleanup(func() { s.CloseActive(context.Background()) })
	return s, store, path
}
func TestDeviceNamesNewSessionIsolationFallbackAndMemoryIdentity(t *testing.T) {
	s, store, path := namedServer(t)
	if _, err := store.SetName(memoryDeviceA, "豆豆"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetName(memoryDeviceB, "星星"); err != nil {
		t.Fatal(err)
	}
	makeSession := func(id string) *Session {
		t.Helper()
		dir, err := s.sessionDirectory(Ownership{DeviceID: id, ClientType: "mac_fixture"})
		if err != nil {
			t.Fatal(err)
		}
		session := s.newSession(nil, dir)
		t.Cleanup(session.cancel)
		return session
	}
	a, b := makeSession(memoryDeviceA), makeSession(memoryDeviceB)
	if a.product.Name() != "豆豆" || b.product.Name() != "星星" || s.product.Name() != "小满" {
		t.Fatal("device names shared")
	}
	if err := a.loadMemory(); err != nil {
		t.Fatal(err)
	}
	rememberedTurn(a, "我们的暗号是蓝风筝", "记住这段话", true)
	if err := a.checkpointMemory(); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetName(memoryDeviceA, "月月"); err != nil {
		t.Fatal(err)
	}
	next := makeSession(memoryDeviceA)
	if err := next.loadMemory(); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains([]byte(memoryContext(t, next)), []byte("蓝风筝")) {
		t.Fatal("rename lost device memory")
	}
	if a.product.Name() != "豆豆" || next.product.Name() != "月月" || a.rec.owner.DeviceID != next.rec.owner.DeviceID || a.rec.owner.SessionID == next.rec.owner.SessionID {
		t.Fatal("rename changed identity or active session")
	}
	if err := os.WriteFile(path, []byte("bad private profile"), 0600); err != nil {
		t.Fatal(err)
	}
	cached := makeSession(memoryDeviceA)
	if cached.product.Name() != "月月" {
		t.Fatal("bad refresh lost name")
	}
	fresh := makeSession("dev_cccccccccccccccccccccccccccccccc")
	if fresh.product.Name() != "小满" {
		t.Fatal("missing device lost base")
	}
	legacy := makeSession("")
	if legacy.product.Name() != "小满" {
		t.Fatal("legacy renamed")
	}
	nilServer := NewServer(Config{DeviceProfilesPath: path}, nil, t.TempDir(), "nil")
	dir, err := nilServer.sessionDirectory(Ownership{})
	if err != nil {
		t.Fatal(err)
	}
	nilSession := nilServer.newSession(nil, dir)
	defer nilSession.cancel()
	if nilSession.product != nil {
		t.Fatal("nil legacy product changed")
	}
}
func TestXiaozhiNamesUseAuthenticatedBinding(t *testing.T) {
	s, store, _ := namedServer(t)
	binding, err := s.devices.BindXiaozhi(testDeviceMAC, testDeviceToken)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetName(binding.ID, "豆豆"); err != nil {
		t.Fatal(err)
	}
	entered := make(chan *Session, 1)
	s.runDevice = func(session *Session) { entered <- session; session.ws.ReadMessage(); session.cancel() }
	h := httptest.NewServer(s.DeviceHandler(DeviceConfig{Token: testDeviceToken, AllowedDevice: testDeviceMAC}))
	defer h.Close()
	c, _, err := deviceDial(h.URL, testDeviceMAC, testDeviceToken)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	deviceHello(t, c)
	session := nextSession(t, entered)
	if session.product.Name() != "豆豆" || session.rec.owner.DeviceID != binding.ID {
		t.Fatal("name was not authenticated")
	}
}
func TestMemoryClearUsesCurrentNameAndKeepsRejectionBoundaries(t *testing.T) {
	for _, text := range []string{"清除聊天记忆", "豆豆，请清空聊天记忆。", "豆豆，清除聊天记忆"} {
		if !memoryClearCommand(text, "豆豆") {
			t.Errorf("rejected clear: %s", text)
		}
	}
	for _, text := range []string{"小满，请清除聊天记忆", "豆豆星清除聊天记忆", "豆豆，清除聊天记忆？", "豆豆，清除聊天记忆然后调音量", "豆豆，清除另一台设备的聊天记忆", "豆豆，‘清除聊天记忆’", "豆豆，清除聊天记忆吗"} {
		if memoryClearCommand(text, "豆豆") {
			t.Errorf("unsafe clear: %s", text)
		}
	}
	if memoryClearCommand("小满清除聊天记忆", "") {
		t.Fatal("hardcoded default prefix retained")
	}
}

func TestMemoryClearAcceptsBareCommandsWhenNameOverlapsCommand(t *testing.T) {
	for _, name := range []string{"清除", "清除聊天记忆", "请"} {
		if !memoryClearCommand("清除聊天记忆", name) {
			t.Errorf("bare command rejected for name %s", name)
		}
	}
}

func TestConversationRenameCannotPersistProductName(t *testing.T) {
	s, store, path := namedServer(t)
	if _, err := store.SetName(memoryDeviceA, "豆豆"); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	dir, err := s.sessionDirectory(Ownership{DeviceID: memoryDeviceA, ClientType: "mac_fixture"})
	if err != nil {
		t.Fatal(err)
	}
	session := s.newSession(nil, dir)
	defer session.cancel()
	if err := session.loadMemory(); err != nil {
		t.Fatal(err)
	}
	rt := rememberedTurn(session, "请把你的正式名字永久改成月月", "", false)
	if _, _, err = session.routeMemory(rt, realtimestate.ContextFor(nil)); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) || session.product.Name() != "豆豆" {
		t.Fatal("conversation changed profile", err)
	}
}

func TestMemoryClearPunctuationNameDoesNotDisableBodyGuards(t *testing.T) {
	for _, name := range []string{`豆豆?`, `豆豆"`, `豆豆？`, `“豆豆”`} {
		if _, err := deviceprofile.NormalizeName(name); err != nil {
			t.Fatal("fixture name must be valid", err)
		}
		for _, body := range []string{"清除聊天记忆", "请清空聊天记忆。"} {
			if !memoryClearCommand(name+"，"+body, name) {
				t.Errorf("valid punctuation name rejected: name=%q body=%q", name, body)
			}
		}
		for _, body := range []string{"清除聊天记忆？", `"清除聊天记忆"`, "‘清除聊天记忆’", "清除\n聊天记忆", "清除聊天记忆然后调音量", "清除另一台设备的聊天记忆"} {
			if memoryClearCommand(name+"，"+body, name) {
				t.Errorf("unsafe body after punctuation name accepted: name=%q body=%q", name, body)
			}
			if memoryClearCommand(body, name) {
				t.Errorf("unsafe bare body accepted: name=%q body=%q", name, body)
			}
		}
	}
}

func TestMemoryClearNameRequiresExplicitComma(t *testing.T) {
	// Names can overlap ordinary negation or reported speech. Without an
	// explicit vocative delimiter, removing the name could reverse intent.
	for _, name := range []string{"不要", "别", "他说", "不许", "豆豆", "小满", `豆豆?`, `豆豆"`} {
		for _, suffix := range []string{"清除聊天记忆", " 清除聊天记忆"} {
			if memoryClearCommand(name+suffix, name) {
				t.Errorf("undelimited name changed deletion intent: name=%q suffix=%q", name, suffix)
			}
		}
		for _, delimiter := range []string{"，", ",", " ， ", " , "} {
			if !memoryClearCommand(name+delimiter+"请清除聊天记忆", name) {
				t.Errorf("explicit name delimiter rejected: name=%q delimiter=%q", name, delimiter)
			}
		}
		for _, body := range []string{"清除聊天记忆？", `"清除聊天记忆"`, "清除\n聊天记忆", "清除聊天记忆然后调音量", "清除另一台设备的聊天记忆"} {
			if memoryClearCommand(name+","+body, name) {
				t.Errorf("unsafe delimited body accepted: name=%q body=%q", name, body)
			}
		}
	}
}
