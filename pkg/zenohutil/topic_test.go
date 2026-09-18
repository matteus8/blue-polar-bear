package zenohutil

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/mcamacho/edgeCompute/pkg/schema"
)

func TestTopicKeyBuilderAndParser(t *testing.T) {
	key, err := BuildTelemetryKey(schema.Tier2Restricted, "drone", "blue", "bravo")
	if err != nil {
		t.Fatalf("building key: %v", err)
	}

	expectedKey := "sec/tier2/drone/blue/bravo/telemetry"
	if key != expectedKey {
		t.Errorf("expected %s, got %s", expectedKey, key)
	}

	info, err := ParseKey(key)
	if err != nil {
		t.Fatalf("parsing key: %v", err)
	}

	if info.TierKey != "tier2" || info.VehicleType != "drone" || info.Team != "blue" || info.UnitID != "bravo" || info.StreamType != "telemetry" {
		t.Errorf("unexpected parsed info: %+v", info)
	}

	// Test invalid key
	if _, err := ParseKey("invalid/key/expression"); err == nil {
		t.Errorf("expected error for invalid key")
	}
}

func TestMemoryBus_PubSub(t *testing.T) {
	bus := NewMemoryBus()
	defer bus.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(1)

	var receivedKey string
	var receivedData string

	err := bus.Subscribe(ctx, "sec/tier1/*/*/*/telemetry", func(key string, payload []byte) {
		receivedKey = key
		receivedData = string(payload)
		wg.Done()
	})
	if err != nil {
		t.Fatalf("subscribing: %v", err)
	}

	testKey := "sec/tier1/drone/blue/alpha/telemetry"
	testPayload := []byte(`{"test":"ok"}`)
	if err := bus.Publish(ctx, testKey, testPayload); err != nil {
		t.Fatalf("publishing: %v", err)
	}

	wg.Wait()

	if receivedKey != testKey {
		t.Errorf("expected key %s, got %s", testKey, receivedKey)
	}
	if receivedData != string(testPayload) {
		t.Errorf("expected payload %s, got %s", string(testPayload), receivedData)
	}
}
