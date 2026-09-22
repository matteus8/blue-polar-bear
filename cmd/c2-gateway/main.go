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
	"sync"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
	"github.com/mcamacho/edgeCompute/pkg/schema"
	"github.com/mcamacho/edgeCompute/pkg/zenohutil"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow cross-origin for tactical field ops & Cloudflare tunnel
	},
}

// VehicleState holds the latest telemetry and breadcrumb history for an asset.
type VehicleState struct {
	Telemetry   schema.TelemetryPayload `json:"telemetry"`
	Header      schema.SecurityHeader   `json:"header"`
	LastSeen    time.Time               `json:"last_seen"`
	TrackPoints []schema.Coordinates    `json:"track_points"`
}

// Hub coordinates connected WebSocket clients.
type Hub struct {
	clients    map[*websocket.Conn]bool
	broadcast  chan []byte
	register   chan *websocket.Conn
	unregister chan *websocket.Conn
	mu         sync.Mutex
}

func newHub() *Hub {
	return &Hub{
		clients:    make(map[*websocket.Conn]bool),
		broadcast:  make(chan []byte, 256),
		register:   make(chan *websocket.Conn),
		unregister: make(chan *websocket.Conn),
	}
}

func (h *Hub) run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			h.mu.Lock()
			for client := range h.clients {
				_ = client.Close()
				delete(h.clients, client)
			}
			h.mu.Unlock()
			return

		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = true
			h.mu.Unlock()
			log.Printf("[C2 Hub] New operator dashboard connected (Total: %d)", len(h.clients))

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				_ = client.Close()
			}
			h.mu.Unlock()
			log.Printf("[C2 Hub] Operator dashboard disconnected (Total: %d)", len(h.clients))

		case message := <-h.broadcast:
			h.mu.Lock()
			for client := range h.clients {
				if err := client.WriteMessage(websocket.TextMessage, message); err != nil {
					_ = client.Close()
					delete(h.clients, client)
				}
			}
			h.mu.Unlock()
		}
	}
}

// Gateway encapsulates the C2 gateway server state.
type Gateway struct {
	bus         zenohutil.Bus
	hub         *Hub
	fleetMu     sync.RWMutex
	fleet       map[string]*VehicleState
	cdsProxyURL string
	mule        *zenohutil.DataMule
}

func NewGateway(bus zenohutil.Bus, cdsProxyURL string, muleOptional ...*zenohutil.DataMule) *Gateway {
	var mule *zenohutil.DataMule
	if len(muleOptional) > 0 && muleOptional[0] != nil {
		mule = muleOptional[0]
	} else {
		mule, _ = zenohutil.NewDataMule("logs/data-mule-spool.jsonl", 5000, "relay.platformstaq.com", bus)
	}
	return &Gateway{
		bus:         bus,
		hub:         newHub(),
		fleet:       make(map[string]*VehicleState),
		cdsProxyURL: cdsProxyURL,
		mule:        mule,
	}
}

func (g *Gateway) handleTelemetry(key string, payload []byte) {
	env, err := zenohutil.ParseJSONEnvelope(payload)
	if err != nil || env.Telemetry == nil {
		return
	}

	// Ingest into Tactical Data Mule (spools if Starlink backhaul is offline/DDIL)
	if g.mule != nil {
		_ = g.mule.Ingest(context.Background(), key, payload)
	}

	g.fleetMu.Lock()
	vID := env.Telemetry.VehicleID
	st, exists := g.fleet[vID]
	if !exists {
		st = &VehicleState{
			TrackPoints: make([]schema.Coordinates, 0, 100),
		}
		g.fleet[vID] = st
	}

	st.Telemetry = *env.Telemetry
	st.Header = env.Header
	st.LastSeen = time.Now().UTC()

	// Append breadcrumb point (limit max 50 points)
	st.TrackPoints = append(st.TrackPoints, env.Telemetry.Coordinates)
	if len(st.TrackPoints) > 50 {
		st.TrackPoints = st.TrackPoints[1:]
	}
	g.fleetMu.Unlock()

	// Broadcast envelope over WebSockets
	g.hub.broadcast <- payload
}

func (g *Gateway) handleFleet(w http.ResponseWriter, r *http.Request) {
	g.fleetMu.RLock()
	defer g.fleetMu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(g.fleet)
}

func (g *Gateway) handleBackhaul(w http.ResponseWriter, r *http.Request) {
	if g.mule == nil {
		http.Error(w, "data mule not configured", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(g.mule.Metrics())
}

func (g *Gateway) handleBackhaulSimulate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if g.mule == nil {
		http.Error(w, "data mule not configured", http.StatusServiceUnavailable)
		return
	}
	var req struct {
		DDILActive bool `json:"ddil_active"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid json: %v", err), http.StatusBadRequest)
		return
	}
	metrics := g.mule.SetSimulationMode(r.Context(), req.DDILActive)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(metrics)
}

func (g *Gateway) handleCommand(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		TargetVehicle string         `json:"target_vehicle"`
		CommandType   string         `json:"command_type"`
		Parameters    map[string]any `json:"parameters"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid json: %v", err), http.StatusBadRequest)
		return
	}

	if req.TargetVehicle == "" || req.CommandType == "" {
		http.Error(w, "target_vehicle and command_type required", http.StatusBadRequest)
		return
	}

	cmdPayload := schema.CommandPayload{
		CommandID:     fmt.Sprintf("cmd-%d", time.Now().UnixNano()),
		TargetVehicle: req.TargetVehicle,
		CommandType:   req.CommandType,
		Parameters:    req.Parameters,
		IssuedAtNs:    time.Now().UnixNano(),
		Issuer:        "Tactical-COP-Operator",
	}

	// Sign command envelope with TIER-2: RESTRICTED for tactical operations
	cmdEnv, err := schema.NewCommandEnvelope(schema.Tier2Restricted, "tactical-c2-gateway", cmdPayload, []string{"OPERATOR_COMMAND"})
	if err != nil {
		http.Error(w, fmt.Sprintf("generating command envelope: %v", err), http.StatusInternalServerError)
		return
	}

	cmdBytes, err := json.Marshal(cmdEnv)
	if err != nil {
		http.Error(w, fmt.Sprintf("marshalling command: %v", err), http.StatusInternalServerError)
		return
	}

	// Topic key: sec/tier2/drone/blue/<target>/command
	topic := zenohutil.BuildKey("tier2", "drone", "blue", req.TargetVehicle, "command")
	if err := g.bus.Publish(r.Context(), topic, cmdBytes); err != nil {
		http.Error(w, fmt.Sprintf("publishing command to Zenoh: %v", err), http.StatusInternalServerError)
		return
	}

	log.Printf("[C2 GATEWAY] Dispatched Command %s to %s on %s", req.CommandType, req.TargetVehicle, topic)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":     "DISPATCHED",
		"command_id": cmdPayload.CommandID,
		"topic":      topic,
	})
}

func (g *Gateway) handleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		return
	}

	g.hub.register <- conn

	go func() {
		defer func() {
			g.hub.unregister <- conn
		}()
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				break
			}
		}
	}()
}

// BackhaulWSMessage is broadcast to operator dashboards over WebSocket.
type BackhaulWSMessage struct {
	Type    string                    `json:"type"`
	Metrics zenohutil.BackhaulMetrics `json:"metrics"`
}

func main() {
	routerURL := flag.String("router", "http://127.0.0.1:8000", "Zenoh REST router URL")
	port := flag.Int("port", 8080, "Tactical C2 Gateway HTTP/WS port")
	webDir := flag.String("web-dir", "web", "Directory containing Tactical COP dashboard")
	cdsProxyURL := flag.String("cds-url", "http://127.0.0.1:8081", "CDS Guard URL for DLQ records")
	mockBus := flag.Bool("mock", false, "Use in-memory bus rather than live Zenoh router")
	spoolFile := flag.String("spool-file", "logs/data-mule-spool.jsonl", "Data mule spool file path for DDIL queueing")
	cloudTarget := flag.String("cloud-target", "relay.platformstaq.com", "Upstream cloud relay domain")
	telemetrySelector := flag.String("telemetry-selector", "sec/tier2/**", "Zenoh topic selector for telemetry ingestion (e.g. sec/tier2/** for tactical high-res or sec/tier1/** for coarsened)")
	flag.Parse()

	log.Printf("Starting Blue Polar Bear Tactical C2 Gateway on port %d...", *port)

	var bus zenohutil.Bus
	if *mockBus {
		log.Printf("C2 Gateway using in-memory bus")
		bus = zenohutil.NewMemoryBus()
	} else {
		log.Printf("C2 Gateway connecting to Zenoh router at: %s", *routerURL)
		bus = zenohutil.NewRESTClient(*routerURL)
	}
	defer bus.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mule, err := zenohutil.NewDataMule(*spoolFile, 5000, *cloudTarget, bus)
	if err != nil {
		log.Printf("[DATA MULE] Warning: initializing data mule: %v", err)
	}

	gw := NewGateway(bus, *cdsProxyURL, mule)
	go gw.hub.run(ctx)

	// Broadcast satellite link metrics to dashboards every 2 seconds
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if gw.mule != nil {
					msg, err := json.Marshal(BackhaulWSMessage{
						Type:    "backhaul_status",
						Metrics: gw.mule.Metrics(),
					})
					if err == nil {
						gw.hub.broadcast <- msg
					}
				}
			}
		}
	}()

	// Subscribe to telemetry stream
	log.Printf("Subscribing to telemetry stream: %s", *telemetrySelector)
	err = bus.Subscribe(ctx, *telemetrySelector, func(key string, payload []byte) {
		gw.handleTelemetry(key, payload)
	})
	if err != nil {
		log.Fatalf("subscribing to telemetry stream: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/fleet", gw.handleFleet)
	mux.HandleFunc("/api/v1/command", gw.handleCommand)
	mux.HandleFunc("/api/v1/backhaul", gw.handleBackhaul)
	mux.HandleFunc("/api/v1/backhaul/simulate", gw.handleBackhaulSimulate)
	mux.HandleFunc("/ws/telemetry", gw.handleWS)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "ONLINE", "service": "c2-gateway"})
	})

	// Static COP Dashboard files
	fs := http.FileServer(http.Dir(*webDir))
	mux.Handle("/", fs)

	server := &http.Server{
		Addr:              fmt.Sprintf(":%d", *port),
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		log.Printf("Tactical COP Dashboard live at: http://127.0.0.1:%d", *port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("Gateway server error: %v", err)
		}
	}()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	log.Printf("Shutting down C2 Gateway...")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer shutdownCancel()
	_ = server.Shutdown(shutdownCtx)
	if gw.mule != nil {
		_ = gw.mule.Close()
	}
	log.Printf("C2 Gateway stopped gracefully.")
}
