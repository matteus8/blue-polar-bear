package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/mcamacho/edgeCompute/pkg/policy"
	"github.com/mcamacho/edgeCompute/pkg/zenohutil"
)

// CDSStats tracks operational counters for the Zero-Trust Guard.
type CDSStats struct {
	IngressTotal uint64 `json:"ingress_total"`
	Passed       uint64 `json:"passed"`
	Redacted     uint64 `json:"redacted"`
	Quarantined  uint64 `json:"quarantined"`
}

type CDSGuard struct {
	bus       zenohutil.Bus
	engine    *policy.PolicyEngine
	dlq       *policy.DeadLetterQueue
	stats     CDSStats
	ingressWg sync.WaitGroup
}

func NewCDSGuard(bus zenohutil.Bus, dlq *policy.DeadLetterQueue, gpsDecimals int) *CDSGuard {
	return &CDSGuard{
		bus:    bus,
		engine: policy.NewPolicyEngine(gpsDecimals),
		dlq:    dlq,
	}
}

// ProcessPacket implements the zero-trust evaluation pipeline.
func (g *CDSGuard) ProcessPacket(ctx context.Context, sourceKey string, raw []byte) {
	atomic.AddUint64(&g.stats.IngressTotal, 1)

	// Step 1: Strict JSON Parse
	env, err := zenohutil.ParseJSONEnvelope(raw)
	if err != nil {
		atomic.AddUint64(&g.stats.Quarantined, 1)
		rec := g.dlq.Quarantine(
			sourceKey,
			policy.ReasonMalformedJSON,
			fmt.Sprintf("JSON unmarshal error: %v", err),
			"unknown",
			"UNKNOWN",
			string(raw),
		)
		log.Printf("[CDS GUARD - QUARANTINE] Record: %s | Topic: %s | Reason: %s", rec.RecordID, sourceKey, rec.RejectionReason)
		return
	}

	// Step 2: Policy Evaluation (Integrity digest check + tier classification)
	action, reason, err := g.engine.Evaluate(env)
	if action == policy.ActionQuarantine {
		atomic.AddUint64(&g.stats.Quarantined, 1)
		rec := g.dlq.Quarantine(
			sourceKey,
			reason,
			fmt.Sprintf("Policy violation: %v", err),
			env.Header.OriginEnclave,
			env.Header.Classification,
			string(raw),
		)
		log.Printf("[CDS GUARD - QUARANTINE] Record: %s | Topic: %s | Tier: %s | Reason: %s (%v)",
			rec.RecordID, sourceKey, env.Header.Classification, rec.RejectionReason, err)
		return
	}

	// Step 3: Enforcement Action
	switch action {
	case policy.ActionPass:
		atomic.AddUint64(&g.stats.Passed, 1)
		log.Printf("[CDS GUARD - PASS] Topic: %s | Tier: %s | Vehicle: %s",
			sourceKey, env.Header.Classification, env.Telemetry.VehicleID)
		// For TIER-1, if arriving from edge, ensure it's mirrored to egress
		egressKey := sourceKey
		if err := g.bus.Publish(ctx, egressKey, raw); err != nil {
			log.Printf("[CDS GUARD - ERROR] Republishing passed packet: %v", err)
		}

	case policy.ActionRedactPass:
		atomic.AddUint64(&g.stats.Redacted, 1)
		sanitizedEnv, err := g.engine.RedactAndSanitize(env)
		if err != nil {
			atomic.AddUint64(&g.stats.Quarantined, 1)
			g.dlq.Quarantine(sourceKey, policy.ReasonUnknownPolicyViolation, err.Error(), env.Header.OriginEnclave, env.Header.Classification, string(raw))
			log.Printf("[CDS GUARD - ERROR] Redaction failure: %v", err)
			return
		}

		sanitizedBytes, err := json.Marshal(sanitizedEnv)
		if err != nil {
			log.Printf("[CDS GUARD - ERROR] Marshalling sanitized packet: %v", err)
			return
		}

		// Down-tag topic key to tier1
		topicInfo, err := zenohutil.ParseKey(sourceKey)
		var egressKey string
		if err == nil {
			egressKey = zenohutil.BuildKey("tier1", topicInfo.VehicleType, topicInfo.Team, topicInfo.UnitID, topicInfo.StreamType)
		} else {
			egressKey = fmt.Sprintf("sec/tier1/redacted/%s", env.Telemetry.VehicleID)
		}

		log.Printf("[CDS GUARD - REDACT & DOWN-TAG] Input: %s (%s) -> Egress: %s (%s) [Coords Coarsened, Payload Scrubbed]",
			sourceKey, env.Header.Classification, egressKey, sanitizedEnv.Header.Classification)

		if err := g.bus.Publish(ctx, egressKey, sanitizedBytes); err != nil {
			log.Printf("[CDS GUARD - ERROR] Publishing sanitized egress: %v", err)
		}
	}
}

func main() {
	routerURL := flag.String("router", "http://127.0.0.1:8000", "Zenoh REST plugin URL")
	httpPort := flag.Int("http-port", 8081, "CDS Guard HTTP inspection server port")
	auditPath := flag.String("audit-file", "logs/cds-audit.jsonl", "File path for immutable audit log")
	gpsDecimals := flag.Int("coarsen-decimals", 2, "GPS decimal precision for TIER-2 redaction")
	mockBus := flag.Bool("mock", false, "Use in-memory bus rather than live Zenoh router")
	flag.Parse()

	log.Printf("Starting Blue Polar Bear Cross Domain Solution (CDS) Guard...")
	log.Printf("Security Enforcement: FAIL-CLOSED | Redaction Precision: %d decimals", *gpsDecimals)

	// Ensure logs directory exists
	if err := os.MkdirAll(filepath.Dir(*auditPath), 0750); err != nil {
		log.Fatalf("creating audit log directory: %v", err)
	}

	dlq, err := policy.NewDeadLetterQueue(500, *auditPath)
	if err != nil {
		log.Fatalf("initializing DLQ: %v", err)
	}
	defer dlq.Close()

	var bus zenohutil.Bus
	if *mockBus {
		log.Printf("Running with In-Memory Mock Bus")
		bus = zenohutil.NewMemoryBus()
	} else {
		log.Printf("Connecting to Zenoh Router REST plugin at: %s", *routerURL)
		bus = zenohutil.NewRESTClient(*routerURL)
	}
	defer bus.Close()

	guard := NewCDSGuard(bus, dlq, *gpsDecimals)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Ingress subscriber: captures all tactical telemetry streams
	// Topic pattern: sec/*/*/*/*/telemetry
	ingressSelector := "sec/**"
	log.Printf("Subscribing to ingress stream: %s", ingressSelector)

	err = bus.Subscribe(ctx, ingressSelector, func(key string, payload []byte) {
		// Ignore packets that are already sanitized (tier1) or commands to prevent loops
		if strings.HasPrefix(key, "sec/tier1/") || strings.HasSuffix(key, "/command") {
			return
		}
		guard.ProcessPacket(ctx, key, payload)
	})
	if err != nil {
		log.Fatalf("subscribing to ingress: %v", err)
	}

	// HTTP Inspection Server (DLQ, Health, Audit metrics)
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "HEALTHY",
			"stats":  guard.stats,
		})
	})
	mux.HandleFunc("/dlq", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		records := dlq.GetRecords(50)
		_ = json.NewEncoder(w).Encode(records)
	})

	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", *httpPort),
		Handler: mux,
	}

	go func() {
		log.Printf("CDS Inspection API online at http://127.0.0.1:%d", *httpPort)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("HTTP server error: %v", err)
		}
	}()

	// Graceful Shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	log.Printf("Shutdown signal received. Flushed audit logs and stopping CDS Guard...")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer shutdownCancel()
	_ = server.Shutdown(shutdownCtx)
	log.Printf("CDS Guard terminated gracefully.")
}
