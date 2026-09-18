package zenohutil

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/mcamacho/edgeCompute/pkg/schema"
)

// MessageHandler is invoked when a message matching the subscription selector arrives.
type MessageHandler func(key string, payload []byte)

// Bus provides pub/sub abstraction for Zenoh.
type Bus interface {
	Publish(ctx context.Context, key string, payload []byte) error
	Subscribe(ctx context.Context, selector string, handler MessageHandler) error
	Close() error
}

// ==========================================
// In-Memory Bus (for tests & offline fallback)
// ==========================================

type MemoryBus struct {
	mu          sync.RWMutex
	subscribers map[string][]MessageHandler
	closed      bool
}

// NewMemoryBus instantiates an in-memory pub-sub bus matching Zenoh path semantics.
func NewMemoryBus() *MemoryBus {
	return &MemoryBus{
		subscribers: make(map[string][]MessageHandler),
	}
}

func matchesSelector(selector, key string) bool {
	if selector == "**" || selector == key {
		return true
	}

	selParts := strings.Split(selector, "/")
	keyParts := strings.Split(key, "/")

	// Simple wildcard matching: '*' matches one level, '**' matches remainder
	sIdx, kIdx := 0, 0
	for sIdx < len(selParts) && kIdx < len(keyParts) {
		if selParts[sIdx] == "**" {
			return true
		}
		if selParts[sIdx] == "*" || selParts[sIdx] == keyParts[kIdx] {
			sIdx++
			kIdx++
			continue
		}
		return false
	}

	if sIdx == len(selParts) && kIdx == len(keyParts) {
		return true
	}
	if sIdx == len(selParts)-1 && selParts[sIdx] == "**" {
		return true
	}
	return false
}

func (m *MemoryBus) Publish(ctx context.Context, key string, payload []byte) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.closed {
		return fmt.Errorf("bus is closed")
	}

	for selector, handlers := range m.subscribers {
		if matchesSelector(selector, key) {
			for _, h := range handlers {
				go h(key, payload)
			}
		}
	}
	return nil
}

func (m *MemoryBus) Subscribe(ctx context.Context, selector string, handler MessageHandler) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return fmt.Errorf("bus is closed")
	}

	m.subscribers[selector] = append(m.subscribers[selector], handler)
	return nil
}

func (m *MemoryBus) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	m.subscribers = make(map[string][]MessageHandler)
	return nil
}

// ==========================================
// Zenoh REST Plugin Client (HTTP & SSE)
// ==========================================

// RESTClient interfaces with zenohd's built-in REST plugin.
type RESTClient struct {
	baseURL    string
	httpClient *http.Client
}

// NewRESTClient creates a client pointing to zenoh-plugin-rest (e.g. http://localhost:8000).
func NewRESTClient(baseURL string) *RESTClient {
	baseURL = strings.TrimRight(baseURL, "/")
	return &RESTClient{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

// Publish sends an HTTP PUT to the Zenoh REST plugin endpoint.
func (c *RESTClient) Publish(ctx context.Context, key string, payload []byte) error {
	url := fmt.Sprintf("%s/%s", c.baseURL, strings.TrimLeft(key, "/"))
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("creating PUT request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("zenoh REST put failed for %s: %w", key, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("zenoh REST put returned HTTP %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

// Subscribe streams messages from Zenoh REST plugin via Server-Sent Events (SSE).
func (c *RESTClient) Subscribe(ctx context.Context, selector string, handler MessageHandler) error {
	url := fmt.Sprintf("%s/%s", c.baseURL, strings.TrimLeft(selector, "/"))

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
			if err != nil {
				log.Printf("[Zenoh REST] error creating SSE request: %v", err)
				time.Sleep(2 * time.Second)
				continue
			}
			req.Header.Set("Accept", "text/event-stream")

			client := &http.Client{Timeout: 0} // infinite streaming timeout
			resp, err := client.Do(req)
			if err != nil {
				log.Printf("[Zenoh REST] SSE connection failed to %s: %v. Retrying in 2s...", url, err)
				select {
				case <-ctx.Done():
					return
				case <-time.After(2 * time.Second):
					continue
				}
			}

			reader := bufio.NewReader(resp.Body)

			for {
				select {
				case <-ctx.Done():
					resp.Body.Close()
					return
				default:
				}

				line, err := reader.ReadString('\n')
				if err != nil {
					resp.Body.Close()
					log.Printf("[Zenoh REST] SSE stream dropped: %v. Reconnecting...", err)
					break
				}

				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "data:") {
					dataStr := strings.TrimPrefix(line, "data:")
					dataBytes := []byte(strings.TrimSpace(dataStr))

					actualKey := selector
					payloadBytes := dataBytes

					type zenohSSE struct {
						Key   string          `json:"key"`
						Value json.RawMessage `json:"value"`
						Time  string          `json:"time"`
					}

					var sseEvt zenohSSE
					if err := json.Unmarshal(dataBytes, &sseEvt); err == nil && sseEvt.Key != "" {
						actualKey = sseEvt.Key
						var strVal string
						if err := json.Unmarshal(sseEvt.Value, &strVal); err == nil {
							payloadBytes = []byte(strVal)
						} else if len(sseEvt.Value) > 0 {
							payloadBytes = sseEvt.Value
						}
					}

					handler(actualKey, payloadBytes)
				}
			}
			time.Sleep(1 * time.Second)
		}
	}()

	return nil
}

func (c *RESTClient) Close() error {
	return nil
}

// Query performs an on-demand GET to the Zenoh REST selector.
func (c *RESTClient) Query(ctx context.Context, selector string) ([]byte, error) {
	url := fmt.Sprintf("%s/%s", c.baseURL, strings.TrimLeft(selector, "/"))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating GET query: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("zenoh REST query failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("query returned HTTP %d", resp.StatusCode)
	}

	return io.ReadAll(resp.Body)
}

// ParseJSONEnvelope attempts to unmarshal arbitrary payload bytes into a SecurityEnvelope.
func ParseJSONEnvelope(payload []byte) (*schema.SecurityEnvelope, error) {
	var env schema.SecurityEnvelope
	if err := json.Unmarshal(payload, &env); err != nil {
		return nil, fmt.Errorf("unmarshalling envelope: %w", err)
	}
	return &env, nil
}
