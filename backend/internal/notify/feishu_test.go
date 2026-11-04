package notify

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	json "encoding/json/v2"
)

func TestSendText(t *testing.T) {
	for _, tc := range []struct {
		name     string
		response string
		wantErr  bool
	}{
		{"success", `{"code":0,"msg":"success"}`, false},
		{"rejected webhook", `{"code":19024,"msg":"invalid token"}`, true},
		{"invalid response", `not json`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if r.Method != http.MethodPost || r.URL.Path != "/webhook" {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
				}
				if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
					t.Errorf("unexpected content type: %s", r.Header.Get("Content-Type"))
				}
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
				}
				var payload struct {
					Type    string `json:"msg_type"`
					Content struct {
						Text string `json:"text"`
					} `json:"content"`
				}
				if err := json.Unmarshal(body, &payload); err != nil {
					t.Error(err)
				}
				if payload.Type != "text" || payload.Content.Text != "证书续期成功\nexample.com" {
					t.Errorf("unexpected notification payload: %s", body)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, tc.response)
			}))
			defer server.Close()
			err := NewFeishuNotifier(server.URL+"/webhook").SendText(t.Context(), "证书续期成功\nexample.com")
			if (err != nil) != tc.wantErr {
				t.Fatalf("SendText() error = %v, wantErr %v", err, tc.wantErr)
			}
			if requests.Load() != 1 {
				t.Fatalf("expected one webhook request, got %d", requests.Load())
			}
		})
	}
}

func TestSendTextCanceled(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		_, _ = io.WriteString(w, `{"code":0}`)
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := NewFeishuNotifier(server.URL).SendText(ctx, "canceled"); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want context.Canceled", err)
	}
	if requests.Load() != 0 {
		t.Fatal("canceled notification reached the webhook")
	}
}
