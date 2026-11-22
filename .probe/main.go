package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/gorilla/websocket"
)

func main() {
	home, _ := os.UserHomeDir()
	p := filepath.Join(home, "Library/Application Support/FamilyCompanion/Realtime18040/default-mac_native.json")
	raw, err := os.ReadFile(p)
	if err != nil {
		fmt.Println("IDENTITY_ERR", err)
		return
	}
	var id struct {
		DeviceID   string `json:"device_id"`
		Credential string `json:"credential"`
	}
	json.Unmarshal(raw, &id)

	body, _ := json.Marshal(map[string]string{"device_id": id.DeviceID, "client_version": "ownership-v1"})
	req, _ := http.NewRequest("POST", "http://127.0.0.1:18040/pair", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+id.Credential)
	req.Header.Set("Content-Type", "application/json")
	cl := &http.Client{Timeout: 10 * time.Second}
	resp, err := cl.Do(req)
	if err != nil {
		fmt.Println("PAIR_ERR", err)
		return
	}
	pb, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	fmt.Printf("PAIR status=%d body=%s\n", resp.StatusCode, string(pb))
	var pr struct {
		Ticket string `json:"ticket"`
	}
	json.Unmarshal(pb, &pr)
	if pr.Ticket == "" {
		return
	}

	hdr := http.Header{}
	hdr.Set("Authorization", "Bearer "+pr.Ticket)
	c, wsResp, err := websocket.DefaultDialer.Dial("ws://127.0.0.1:18040/live", hdr)
	if err != nil {
		code := 0
		if wsResp != nil {
			code = wsResp.StatusCode
		}
		fmt.Println("WS_DIAL_ERR", code, err)
		return
	}
	defer c.Close()
	fmt.Println("WS_CONNECTED")
	c.SetReadDeadline(time.Now().Add(12 * time.Second))
	for i := 0; i < 6; i++ {
		mt, data, err := c.ReadMessage()
		if err != nil {
			fmt.Println("WS_READ_END", err)
			return
		}
		if mt == websocket.TextMessage {
			s := string(data)
			if len(s) > 300 {
				s = s[:300]
			}
			fmt.Println("TEXT", s)
		} else {
			fmt.Println("BINARY bytes=", len(data))
		}
	}
}
