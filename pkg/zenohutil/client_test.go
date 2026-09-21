package zenohutil

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestMatchesSelector(t *testing.T) {
	tests := []struct {
		selector string
		key      string
		expected bool
	}{
		{"**", "sec/tier1/drone/blue/alpha/telemetry", true},
		{"sec/**", "sec/tier1/drone/blue/alpha/telemetry", true},
		{"sec/tier1/**", "sec/tier1/drone/blue/alpha/telemetry", true},
		{"sec/tier2/**", "sec/tier1/drone/blue/alpha/telemetry", false},
		{"sec/tier1/drone/blue/*/telemetry", "sec/tier1/drone/blue/alpha/telemetry", true},
		{"sec/tier1/drone/blue/*/telemetry", "sec/tier1/drone/red/alpha/telemetry", false},
		{"sec/tier1/drone/blue/alpha/telemetry", "sec/tier1/drone/blue/alpha/telemetry", true},
		{"sec/tier1/drone/blue/alpha/telemetry", "sec/tier1/drone/blue/bravo/telemetry", false},
		{"short/key", "short/key/extra", false},
		{"short/key/**", "short/key/extra", true},
	}

	for _, tt := range tests {
		result := matchesSelector(tt.selector, tt.key)
		if result != tt.expected {
			t.Errorf("matchesSelector(%q, %q) = %v, expected %v", tt.selector, tt.key, result, tt.expected)
		}
	}
}

func TestMemoryBus_Lifecycle(t *testing.T) {
	bus := NewMemoryBus()

	ctx := context.Background()
	received := make(chan []byte, 1)
	err := bus.Subscribe(ctx, "test/**", func(key string, payload []byte) {
		received <- payload
	})
	if err != nil {
		t.Fatalf("subscribing: %v", err)
	}

	testData := []byte("hello-mesh")
	if err := bus.Publish(ctx, "test/topic/1", testData); err != nil {
		t.Fatalf("publishing: %v", err)
	}

	select {
	case msg := <-received:
		if string(msg) != "hello-mesh" {
			t.Errorf("expected hello-mesh, got %s", string(msg))
		}
	case <-time.After(1 * time.Second):
		t.Fatalf("timed out waiting for message")
	}

	if err := bus.Close(); err != nil {
		t.Fatalf("closing bus: %v", err)
	}

	// Operations after close should return error
	if err := bus.Publish(ctx, "test/topic/1", testData); err == nil {
		t.Errorf("expected error publishing to closed bus")
	}
	if err := bus.Subscribe(ctx, "test/**", func(k string, p []byte) {}); err == nil {
		t.Errorf("expected error subscribing to closed bus")
	}
}

func TestRESTClient_PublishAndQuery(t *testing.T) {
	var receivedMethod string
	var receivedPath string
	var receivedBody []byte

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedMethod = r.Method
		receivedPath = r.URL.Path
		buf := make([]byte, 1024)
		n, _ := r.Body.Read(buf)
		receivedBody = buf[:n]

		if r.URL.Path == "/sec/tier1/status" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"ONLINE"}`))
			return
		}

		if r.URL.Path == "/error/500" {
			http.Error(w, "internal router error", http.StatusInternalServerError)
			return
		}

		if r.Method == http.MethodPut && r.URL.Path == "/sec/tier2/drone/blue/telemetry" {
			w.WriteHeader(http.StatusOK)
			return
		}

		http.NotFound(w, r)
	}))
	defer server.Close()

	client := NewRESTClient(server.URL)
	defer client.Close()
	ctx := context.Background()

	// 1. Test Publish PUT
	payload := []byte(`{"test":true}`)
	if err := client.Publish(ctx, "sec/tier2/drone/blue/telemetry", payload); err != nil {
		t.Fatalf("publish failed: %v", err)
	}
	if receivedMethod != http.MethodPut {
		t.Errorf("expected method PUT, got %s", receivedMethod)
	}
	if receivedPath != "/sec/tier2/drone/blue/telemetry" {
		t.Errorf("expected path /sec/tier2/drone/blue/telemetry, got %s", receivedPath)
	}
	if string(receivedBody) != string(payload) {
		t.Errorf("expected payload %s, got %s", string(payload), string(receivedBody))
	}

	// 2. Test Publish failure on HTTP error
	if err := client.Publish(ctx, "error/500", payload); err == nil {
		t.Errorf("expected error on HTTP 500")
	}

	// 3. Test Query GET
	queryData, err := client.Query(ctx, "sec/tier1/status")
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if string(queryData) != `{"status":"ONLINE"}` {
		t.Errorf("unexpected query response: %s", string(queryData))
	}

	// 4. Test Query failure
	if _, err := client.Query(ctx, "nonexistent"); err == nil {
		t.Errorf("expected error querying nonexistent endpoint")
	}
}

func TestRESTClient_SubscribeSSE(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)

		flusher, ok := w.(http.Flusher)
		if !ok {
			return
		}

		// Emit SSE telemetry event
		sseMsg := `data: {"key":"sec/tier1/drone/blue/alpha/telemetry","value":"{\"status\":\"OK\"}","time":"2026-09-21T21:45:00Z"}` + "\n\n"
		_, _ = fmt.Fprint(w, sseMsg)
		flusher.Flush()
	}))
	defer server.Close()

	client := NewRESTClient(server.URL)
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	receivedChan := make(chan []byte, 1)
	err := client.Subscribe(ctx, "sec/tier1/**", func(key string, payload []byte) {
		receivedChan <- payload
	})
	if err != nil {
		t.Fatalf("subscribing to SSE: %v", err)
	}

	select {
	case payload := <-receivedChan:
		if string(payload) != `{"status":"OK"}` {
			t.Errorf("unexpected payload from SSE: %s", string(payload))
		}
	case <-time.After(1 * time.Second):
		t.Fatalf("timed out waiting for SSE event")
	}
}
