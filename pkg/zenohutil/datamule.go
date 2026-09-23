package zenohutil

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// BackhaulStatus represents the operational state of the satellite link.
type BackhaulStatus string

const (
	BackhaulOnline        BackhaulStatus = "ONLINE"
	BackhaulDegraded      BackhaulStatus = "DEGRADED"
	BackhaulDDILBuffering BackhaulStatus = "DDIL_BUFFERING"
)

// SpooledPacket holds an immutable buffered packet waiting for satellite backhaul.
type SpooledPacket struct {
	ID        string    `json:"id"`
	Topic     string    `json:"topic"`
	Payload   []byte    `json:"payload"`
	Timestamp time.Time `json:"timestamp"`
}

// BackhaulMetrics exposes real-time satellite link and data mule telemetry.
type BackhaulMetrics struct {
	Status         BackhaulStatus `json:"status"`
	CloudTarget    string         `json:"cloud_target"`
	LatencyMs      int64          `json:"latency_ms"`
	SpooledPackets int            `json:"spooled_packets"`
	SpooledBytes   int64          `json:"spooled_bytes"`
	SyncedTotal    int64          `json:"synced_total"`
	LastSyncTime   time.Time      `json:"last_sync_time"`
	SimulatedMode  bool           `json:"simulated_mode"`
}

// DataMule manages store-and-forward queueing across satellite dropouts.
type DataMule struct {
	mu            sync.RWMutex
	status        BackhaulStatus
	cloudTarget   string
	spoolFile     *os.File
	spoolFilePath string
	buffer        []SpooledPacket
	maxBuffer     int
	latencyMs     int64
	syncedTotal   int64
	lastSyncTime  time.Time
	simulatedDDIL bool
	upstreamBus   Bus
	httpClient    *http.Client
}

// NewDataMule initializes the Tactical Data Mule store-and-forward engine.
func NewDataMule(spoolFilePath string, maxBuffer int, cloudTarget string, upstreamBus Bus) (*DataMule, error) {
	if maxBuffer <= 0 {
		maxBuffer = 5000
	}

	dm := &DataMule{
		status:        BackhaulOnline,
		cloudTarget:   cloudTarget,
		spoolFilePath: spoolFilePath,
		buffer:        make([]SpooledPacket, 0, 100),
		maxBuffer:     maxBuffer,
		latencyMs:     38, // Simulated nominal Starlink latency
		lastSyncTime:  time.Now(),
		upstreamBus:   upstreamBus,
		httpClient: &http.Client{
			Timeout: 3 * time.Second,
		},
	}

	if spoolFilePath != "" {
		cleanPath := filepath.Clean(spoolFilePath)
		dir := filepath.Dir(cleanPath)
		if err := os.MkdirAll(dir, 0750); err != nil {
			return nil, fmt.Errorf("creating spool directory %q: %w", dir, err)
		}
		// #nosec G304 - configured spool path is cleaned
		f, err := os.OpenFile(cleanPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			return nil, fmt.Errorf("opening spool log file %q: %w", cleanPath, err)
		}
		dm.spoolFile = f
	}

	return dm, nil
}

// Ingest handles incoming sanitized telemetry. If backhaul is down, packets are spooled.
func (dm *DataMule) Ingest(ctx context.Context, topic string, payload []byte) error {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	// If backhaul is online and not in simulated DDIL blackout, forward directly
	if dm.status == BackhaulOnline && !dm.simulatedDDIL && dm.upstreamBus != nil {
		upstreamTopic := formatUpstreamTopic(topic)
		err := dm.upstreamBus.Publish(ctx, upstreamTopic, payload)
		if err == nil {
			dm.syncedTotal++
			dm.lastSyncTime = time.Now()
			return nil
		}
		// Failure upstream triggers automatic transition to DDIL buffering
		log.Printf("[DATA MULE] Upstream backhaul failed: %v. Entering DDIL buffering mode.", err)
		dm.status = BackhaulDDILBuffering
	}

	// Buffer packet in Data Mule FIFO spool
	pkt := SpooledPacket{
		ID:        fmt.Sprintf("mule-%d-%d", time.Now().UnixNano(), len(dm.buffer)+1),
		Topic:     topic,
		Payload:   payload,
		Timestamp: time.Now(),
	}

	if len(dm.buffer) >= dm.maxBuffer {
		// Drop oldest packet if ring buffer capacity exceeded
		dm.buffer = dm.buffer[1:]
	}
	dm.buffer = append(dm.buffer, pkt)

	// Persist to disk spool for DDIL survivability
	if dm.spoolFile != nil {
		raw, _ := json.Marshal(pkt)
		_, _ = dm.spoolFile.WriteString(string(raw) + "\n")
	}

	return nil
}

// Flush synchronizes all spooled packets upstream once satellite connectivity is restored.
func (dm *DataMule) Flush(ctx context.Context) (int, error) {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	if len(dm.buffer) == 0 {
		dm.status = BackhaulOnline
		return 0, nil
	}

	flushed := 0
	for _, pkt := range dm.buffer {
		if dm.upstreamBus != nil {
			upstreamTopic := formatUpstreamTopic(pkt.Topic)
			if err := dm.upstreamBus.Publish(ctx, upstreamTopic, pkt.Payload); err != nil {
				log.Printf("[DATA MULE] Failed to flush packet %s: %v", pkt.ID, err)
				return flushed, err
			}
		}
		flushed++
		dm.syncedTotal++
	}

	// Clear in-memory buffer
	dm.buffer = dm.buffer[:0]
	dm.lastSyncTime = time.Now()
	dm.status = BackhaulOnline

	// Truncate spool file after successful flush
	if dm.spoolFile != nil {
		_ = dm.spoolFile.Close()
		cleanPath := filepath.Clean(dm.spoolFilePath)
		// #nosec G304 - configured spool path is cleaned
		f, err := os.OpenFile(cleanPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
		if err == nil {
			dm.spoolFile = f
		}
	}

	log.Printf("[DATA MULE] Satellite backhaul restored! Successfully flushed %d spooled packets upstream.", flushed)
	return flushed, nil
}

// SetSimulationMode allows operators to simulate Starlink dropouts on demand.
func (dm *DataMule) SetSimulationMode(ctx context.Context, ddilActive bool) BackhaulMetrics {
	dm.mu.Lock()
	dm.simulatedDDIL = ddilActive
	if ddilActive {
		dm.status = BackhaulDDILBuffering
		dm.latencyMs = 9999
		log.Printf("[DATA MULE] SIMULATION: Starlink backhaul disconnected (Tactical DDIL Active)")
		dm.mu.Unlock()
	} else {
		dm.status = BackhaulOnline
		dm.latencyMs = 42
		log.Printf("[DATA MULE] SIMULATION: Starlink satellite backhaul restored")
		dm.mu.Unlock()
		_, _ = dm.Flush(ctx)
	}

	return dm.Metrics()
}

// Metrics returns current status and buffer metrics.
func (dm *DataMule) Metrics() BackhaulMetrics {
	dm.mu.RLock()
	defer dm.mu.RUnlock()

	var totalBytes int64
	for _, p := range dm.buffer {
		totalBytes += int64(len(p.Payload))
	}

	return BackhaulMetrics{
		Status:         dm.status,
		CloudTarget:    dm.cloudTarget,
		LatencyMs:      dm.latencyMs,
		SpooledPackets: len(dm.buffer),
		SpooledBytes:   totalBytes,
		SyncedTotal:    dm.syncedTotal,
		LastSyncTime:   dm.lastSyncTime,
		SimulatedMode:  dm.simulatedDDIL,
	}
}

// Close gracefully closes the spool file.
func (dm *DataMule) Close() error {
	dm.mu.Lock()
	defer dm.mu.Unlock()
	if dm.spoolFile != nil {
		return dm.spoolFile.Close()
	}
	return nil
}

// formatUpstreamTopic ensures packets forwarded to cloud backhaul are prefixed with upstream/
// to prevent circular loops or duplicate ingestion on local tactical mesh subscribers.
func formatUpstreamTopic(topic string) string {
	if strings.HasPrefix(topic, "upstream/") {
		return topic
	}
	return "upstream/" + topic
}

