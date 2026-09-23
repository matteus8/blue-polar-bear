package zenohutil

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDataMule_StoreAndForward(t *testing.T) {
	ctx := context.Background()
	bus := NewMemoryBus()
	defer bus.Close()

	spoolDir := t.TempDir()
	spoolFile := filepath.Join(spoolDir, "test-mule.jsonl")

	dm, err := NewDataMule(spoolFile, 50, "relay.platformstaq.com", bus)
	if err != nil {
		t.Fatalf("creating datamule: %v", err)
	}
	defer dm.Close()

	// 1. Initial nominal online state
	metrics := dm.Metrics()
	if metrics.Status != BackhaulOnline {
		t.Errorf("expected online status, got %s", metrics.Status)
	}

	// 2. Ingest online -> forwards directly to upstream backhaul topic
	forwardedChan := make(chan []byte, 10)
	bus.Subscribe(ctx, "upstream/sec/tier1/drone/blue/alpha/telemetry", func(key string, payload []byte) {
		forwardedChan <- payload
	})

	testPayload := []byte(`{"vehicle_id":"alpha","status":"AIRBORNE"}`)
	if err := dm.Ingest(ctx, "sec/tier1/drone/blue/alpha/telemetry", testPayload); err != nil {
		t.Fatalf("ingest failed: %v", err)
	}

	select {
	case <-forwardedChan:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for forwarded packet")
	}

	metrics = dm.Metrics()
	if metrics.SpooledPackets != 0 {
		t.Errorf("expected 0 spooled packets, got %d", metrics.SpooledPackets)
	}
	if metrics.SyncedTotal != 1 {
		t.Errorf("expected 1 synced packet, got %d", metrics.SyncedTotal)
	}

	// 3. Trigger simulated DDIL outage
	dm.SetSimulationMode(ctx, true)
	metrics = dm.Metrics()
	if metrics.Status != BackhaulDDILBuffering {
		t.Errorf("expected DDIL_BUFFERING, got %s", metrics.Status)
	}

	// 4. Ingest during DDIL -> buffered into queue
	for i := 1; i <= 5; i++ {
		pkt := []byte(`{"seq": ` + string(rune('0'+i)) + `}`)
		_ = dm.Ingest(ctx, "sec/tier1/drone/blue/alpha/telemetry", pkt)
	}

	metrics = dm.Metrics()
	if metrics.SpooledPackets != 5 {
		t.Errorf("expected 5 spooled packets, got %d", metrics.SpooledPackets)
	}

	// Check disk persistence
	diskData, err := os.ReadFile(spoolFile)
	if err != nil {
		t.Fatalf("reading spool file: %v", err)
	}
	if len(diskData) == 0 {
		t.Errorf("expected spool file to contain persisted records")
	}

	// 5. Restore satellite connection -> automatic flush
	dm.SetSimulationMode(ctx, false)
	metrics = dm.Metrics()
	if metrics.Status != BackhaulOnline {
		t.Errorf("expected restored online status, got %s", metrics.Status)
	}
	if metrics.SpooledPackets != 0 {
		t.Errorf("expected 0 spooled packets after flush, got %d", metrics.SpooledPackets)
	}
	if metrics.SyncedTotal != 6 {
		t.Errorf("expected 6 total synced packets, got %d", metrics.SyncedTotal)
	}
}
