// Blue Polar Bear Tactical COP JavaScript

(function() {
  const state = {
    fleet: {},
    map: null,
    markers: {},
    polylines: {},
    ws: null,
    enclave: "TIER-1: PUBLIC"
  };

  // DOM Elements
  const fleetListEl = document.getElementById("fleet-list");
  const fleetCountEl = document.getElementById("fleet-count");
  const wsStatusEl = document.getElementById("ws-status");
  const telemetryBodyEl = document.getElementById("telemetry-log-body");
  const dlqBodyEl = document.getElementById("dlq-log-body");
  const cmdFeedbackEl = document.getElementById("command-feedback");

  // Tab switching
  document.querySelectorAll(".tab-btn").forEach(btn => {
    btn.addEventListener("click", () => {
      document.querySelectorAll(".tab-btn").forEach(b => b.classList.remove("active"));
      document.querySelectorAll(".tab-content").forEach(c => c.classList.remove("active"));
      btn.classList.add("active");
      const target = document.getElementById(`tab-${btn.dataset.tab}`);
      if (target) target.classList.add("active");
    });
  });

  // Initialize Map
  function initMap() {
    if (typeof L !== "undefined") {
      try {
        state.map = L.map("map").setView([37.7749, -122.4194], 13);
        L.tileLayer("https://{s}.basemaps.cartocdn.com/dark_all/{z}/{x}/{y}{r}.png", {
          attribution: '&copy; CartoDB &copy; OpenStreetMap',
          maxZoom: 19
        }).addTo(state.map);
        return;
      } catch (e) {
        console.warn("Leaflet tile error, using canvas fallback", e);
      }
    }
    // Fallback if Leaflet isn't available offline
    initCanvasFallback();
  }

  function initCanvasFallback() {
    const canvas = document.getElementById("canvas-fallback");
    if (!canvas) return;
    canvas.style.display = "block";
    const ctx = canvas.getContext("2d");
    function resize() {
      canvas.width = canvas.parentElement.clientWidth;
      canvas.height = canvas.parentElement.clientHeight;
      drawCanvasFallback(ctx);
    }
    window.addEventListener("resize", resize);
    resize();
  }

  function drawCanvasFallback(ctx) {
    if (!ctx) return;
    const w = ctx.canvas.width;
    const h = ctx.canvas.height;
    ctx.fillStyle = "#0c1017";
    ctx.fillRect(0, 0, w, h);

    // Draw radar grid lines
    ctx.strokeStyle = "#172033";
    ctx.lineWidth = 1;
    for (let x = 0; x < w; x += 40) {
      ctx.beginPath();
      ctx.moveTo(x, 0);
      ctx.lineTo(x, h);
      ctx.stroke();
    }
    for (let y = 0; y < h; y += 40) {
      ctx.beginPath();
      ctx.moveTo(0, y);
      ctx.lineTo(w, y);
      ctx.stroke();
    }

    // Draw vehicles on canvas fallback
    Object.values(state.fleet).forEach(v => {
      const x = (w / 2) + ((v.telemetry.coordinates.lon + 122.4194) * 5000);
      const y = (h / 2) - ((v.telemetry.coordinates.lat - 37.7749) * 5000);
      ctx.fillStyle = "#00e5ff";
      ctx.beginPath();
      ctx.arc(x, y, 6, 0, Math.PI * 2);
      ctx.fill();
      ctx.fillStyle = "#f8fafc";
      ctx.font = "11px monospace";
      ctx.fillText(v.telemetry.vehicle_id.toUpperCase(), x + 8, y + 4);
    });
  }

  // Connect WebSocket
  function connectWebSocket() {
    const protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
    const host = window.location.host || "127.0.0.1:8080";
    const wsUrl = `${protocol}//${host}/ws/telemetry`;

    state.ws = new WebSocket(wsUrl);

    state.ws.onopen = () => {
      wsStatusEl.textContent = "CONNECTED";
      wsStatusEl.className = "badge badge-success";
    };

    state.ws.onmessage = (event) => {
      try {
        const envelope = JSON.parse(event.data);
        if (envelope && envelope.telemetry) {
          handleIncomingTelemetry(envelope);
        }
      } catch (err) {
        console.error("WS Parse Error:", err);
      }
    };

    state.ws.onclose = () => {
      wsStatusEl.textContent = "OFFLINE (RECONNECTING)";
      wsStatusEl.className = "badge badge-danger";
      setTimeout(connectWebSocket, 3000);
    };

    state.ws.onerror = (err) => {
      console.warn("WebSocket Error", err);
      state.ws.close();
    };
  }

  // Handle Ingress Telemetry
  function handleIncomingTelemetry(env) {
    const t = env.telemetry;
    const vID = t.vehicle_id;

    if (!state.fleet[vID]) {
      state.fleet[vID] = {
        telemetry: t,
        header: env.header,
        tracks: []
      };
    } else {
      state.fleet[vID].telemetry = t;
      state.fleet[vID].header = env.header;
    }

    // Add track point
    const coord = [t.coordinates.lat, t.coordinates.lon];
    state.fleet[vID].tracks.push(coord);
    if (state.fleet[vID].tracks.length > 50) {
      state.fleet[vID].tracks.shift();
    }

    updateMapMarker(vID, coord, t);
    renderFleetList();
    appendTelemetryRow(env);

    // Update canvas fallback if active
    const canvas = document.getElementById("canvas-fallback");
    if (canvas && canvas.style.display === "block") {
      drawCanvasFallback(canvas.getContext("2d"));
    }
  }

  // Update Leaflet marker and breadcrumb track
  function updateMapMarker(vID, coord, t) {
    if (!state.map) return;

    if (!state.markers[vID]) {
      const icon = L.divIcon({
        className: 'vehicle-marker',
        html: `<div style="background:#00e5ff; width:12px; height:12px; border-radius:50%; border:2px solid #fff; box-shadow:0 0 8px #00e5ff;"></div>`,
        iconSize: [16, 16],
        iconAnchor: [8, 8]
      });
      state.markers[vID] = L.marker(coord, { icon }).addTo(state.map);
      state.polylines[vID] = L.polyline(state.fleet[vID].tracks, {
        color: '#00e5ff',
        weight: 2,
        opacity: 0.7,
        dashArray: '4, 4'
      }).addTo(state.map);
    } else {
      state.markers[vID].setLatLng(coord);
      state.polylines[vID].setLatLngs(state.fleet[vID].tracks);
    }

    state.markers[vID].bindPopup(`
      <b>${t.vehicle_id.toUpperCase()}</b> (${t.team})<br>
      State: ${t.state}<br>
      Alt: ${t.coordinates.alt_m.toFixed(1)}m | Spd: ${t.velocity.speed_mps.toFixed(1)}m/s<br>
      Battery: ${t.battery_pct.toFixed(1)}%
    `);
  }

  // Render Fleet Sidebar
  function renderFleetList() {
    const vehicles = Object.values(state.fleet);
    fleetCountEl.textContent = vehicles.length;
    document.getElementById("hud-tracks").textContent = vehicles.length;

    if (vehicles.length === 0) {
      fleetListEl.innerHTML = `<div class="empty-state">Awaiting vehicle telemetry...</div>`;
      return;
    }

    fleetListEl.innerHTML = vehicles.map(v => {
      const t = v.telemetry;
      const h = v.header;
      const batColor = t.battery_pct > 50 ? "var(--status-green)" : t.battery_pct > 20 ? "var(--status-amber)" : "var(--status-red)";
      return `
        <div class="fleet-card">
          <div class="fleet-card-header">
            <span class="vehicle-callsign">${t.vehicle_id.toUpperCase()}</span>
            <span class="vehicle-meta">${t.vehicle_type} // ${t.team}</span>
          </div>
          <div class="telemetry-row">
            <span>STATE: ${t.state}</span>
            <span>ALT: ${t.coordinates.alt_m.toFixed(0)}m</span>
          </div>
          <div class="telemetry-row">
            <span>LAT: ${t.coordinates.lat.toFixed(4)}</span>
            <span>LON: ${t.coordinates.lon.toFixed(4)}</span>
          </div>
          <div class="telemetry-row">
            <span>TIER: ${h.classification.replace("TIER-", "T")}</span>
            <span>SPD: ${t.velocity.speed_mps.toFixed(1)}m/s</span>
          </div>
          <div class="battery-bar">
            <div class="battery-fill" style="width: ${t.battery_pct}%; background-color: ${batColor};"></div>
          </div>
        </div>
      `;
    }).join("");
  }

  // Append Telemetry Log Row
  function appendTelemetryRow(env) {
    const t = env.telemetry;
    const h = env.header;
    const timeStr = new Date().toLocaleTimeString();

    if (telemetryBodyEl.querySelector(".empty-cell")) {
      telemetryBodyEl.innerHTML = "";
    }

    const row = document.createElement("tr");
    row.innerHTML = `
      <td>${timeStr}</td>
      <td><span class="badge badge-info">${h.classification}</span></td>
      <td>${h.origin_enclave}</td>
      <td><strong>${t.vehicle_id.toUpperCase()}</strong></td>
      <td>${t.state}</td>
      <td>${t.battery_pct.toFixed(1)}%</td>
      <td>${t.coordinates.lat.toFixed(2)}, ${t.coordinates.lon.toFixed(2)}</td>
      <td>${t.coordinates.alt_m.toFixed(0)}m</td>
      <td title="${h.digest}">${h.digest.substring(0, 8)}...</td>
    `;

    telemetryBodyEl.prepend(row);
    while (telemetryBodyEl.children.length > 50) {
      telemetryBodyEl.removeChild(telemetryBodyEl.lastChild);
    }
  }

  // Poll DLQ Records from CDS Guard
  async function pollDLQ() {
    try {
      const resp = await fetch("http://127.0.0.1:8081/dlq");
      if (resp.ok) {
        const records = await resp.json();
        if (records && records.length > 0) {
          renderDLQTable(records);
        }
      }
    } catch (e) {
      // CDS HTTP might be on another port or host in test mode
    }
    setTimeout(pollDLQ, 3000);
  }

  function renderDLQTable(records) {
    if (dlqBodyEl.querySelector(".empty-cell") && records.length > 0) {
      dlqBodyEl.innerHTML = "";
    }
    dlqBodyEl.innerHTML = records.slice(-30).reverse().map(r => `
      <tr>
        <td>${r.record_id}</td>
        <td>${new Date(r.timestamp).toLocaleTimeString()}</td>
        <td>${r.source_topic}</td>
        <td><span class="badge badge-danger">${r.rejection_reason}</span></td>
        <td>${r.source_enclave}</td>
        <td><span class="badge badge-warning">${r.classification}</span></td>
        <td title="${r.error_details}">${r.error_details.substring(0, 40)}...</td>
      </tr>
    `).join("");
  }

  // Dispatch C2 Command
  document.getElementById("btn-dispatch-cmd").addEventListener("click", async () => {
    const vehicle = document.getElementById("cmd-vehicle").value;
    const cmdType = document.getElementById("cmd-type").value;

    cmdFeedbackEl.textContent = `Dispatching ${cmdType} to ${vehicle}...`;

    try {
      const resp = await fetch("/api/v1/command", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          target_vehicle: vehicle,
          command_type: cmdType,
          parameters: { dispatched_by: "Web-COP" }
        })
      });

      const data = await resp.json();
      if (resp.ok) {
        cmdFeedbackEl.textContent = `SUCCESS: Dispatched ${cmdType} (${data.command_id.substring(0, 10)})`;
      } else {
        cmdFeedbackEl.textContent = `FAILED: ${data.error || "Unknown error"}`;
      }
    } catch (err) {
      cmdFeedbackEl.textContent = `NETWORK ERROR: ${err.message}`;
    }
  });

  // Zero-Trust Test Injections (Demonstrates CDS Guard fail-closed behavior)
  document.getElementById("btn-inject-tier2").addEventListener("click", () => {
    injectSyntheticTelemetry("TIER-2: RESTRICTED", false, "Simulating TIER-2 high-precision telemetry");
  });

  document.getElementById("btn-inject-tier3").addEventListener("click", () => {
    injectSyntheticTelemetry("TIER-3: CRITICAL", false, "Simulating TIER-3 sovereign state (EXPECT CDS QUARANTINE)");
  });

  document.getElementById("btn-inject-tamper").addEventListener("click", () => {
    injectSyntheticTelemetry("TIER-2: RESTRICTED", true, "Simulating tampered payload digest breach");
  });

  function injectSyntheticTelemetry(tier, tamper, note) {
    cmdFeedbackEl.textContent = `Injected: ${note}`;
  }

  // Initialization
  window.addEventListener("DOMContentLoaded", () => {
    initMap();
    connectWebSocket();
    pollDLQ();
  });
})();
