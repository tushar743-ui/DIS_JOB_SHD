//go:build integration

package handler_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func dialWebSocket(t *testing.T, url string) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)

	conn, resp, err := websocket.Dial(ctx, url, &websocket.DialOptions{HTTPClient: testClient})
	if err != nil {
		return nil, resp, err
	}
	t.Cleanup(func() { conn.CloseNow() })
	return conn, resp, nil
}

func readFrame(t *testing.T, conn *websocket.Conn, timeout time.Duration) []byte {
	t.Helper()
	if timeout <= 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	_, data, err := conn.Read(ctx)
	if err != nil {
		return nil
	}
	return data
}

func readEventType(t *testing.T, conn *websocket.Conn, timeout time.Duration) string {
	t.Helper()
	raw := readFrame(t, conn, timeout)
	if raw == nil {
		return ""
	}
	var evt struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(raw, &evt); err != nil {
		return ""
	}
	return evt.Type
}
