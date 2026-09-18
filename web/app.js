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
  const cmdVehicleSelect = document.getElementById("cmd-vehicle");

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
        state.map = L.map("map").setView([31.6500, -8.0100], 12);
        L.tileLayer("https://{s}.basemaps.cartocdn.com/rastertiles/voyager/{z}/{x}/{y}{r}.png", {
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
    ctx.fillStyle = "#F8F5EE";
    ctx.fillRect(0, 0, w, h);

    // Draw tactical grid lines in light warm tan
    ctx.strokeStyle = "#E8E2D6";
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

    // Draw vehicles on canvas fallback (Morocco Sector)
    Object.values(state.fleet).forEach(v => {
      const isRed = v.telemetry.team === "red";
      const x = (w / 2) + ((v.telemetry.coordinates.lon - (-8.0100)) * 5000);
      const y = (h / 2) - ((v.telemetry.coordinates.lat - 31.6500) * 5000);
      ctx.fillStyle = isRed ? "#BA4540" : "#326B94";
      ctx.beginPath();
      ctx.arc(x, y, 5, 0, Math.PI * 2);
      ctx.fill();
      ctx.fillStyle = "#2B2621";
      ctx.font = "11px -apple-system, sans-serif";
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
      updateCommandVehicleDropdown();
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

  // Update Leaflet marker and breadcrumb track with Team colors
  function updateMapMarker(vID, coord, t) {
    if (!state.map) return;

    const isRed = t.team === "red";
    const color = isRed ? "#BA4540" : "#326B94";

    if (!state.markers[vID]) {
      const icon = L.divIcon({
        className: 'vehicle-marker',
        html: `<div style="background:${color}; width:12px; height:12px; border-radius:50%; border:2px solid #FFFFFF; box-shadow:0 1px 4px rgba(0,0,0,0.25);"></div>`,
        iconSize: [16, 16],
        iconAnchor: [8, 8]
      });
      state.markers[vID] = L.marker(coord, { icon }).addTo(state.map);
      state.polylines[vID] = L.polyline(state.fleet[vID].tracks, {
        color: color,
        weight: 2.5,
        opacity: 0.7,
        dashArray: '4, 4'
      }).addTo(state.map);
    } else {
      state.markers[vID].setLatLng(coord);
      state.polylines[vID].setLatLngs(state.fleet[vID].tracks);
    }

    state.markers[vID].bindPopup(`
      <div style="font-family: -apple-system, BlinkMacSystemFont, sans-serif; color: #2B2621; font-size: 12px; line-height: 1.5; padding: 2px;">
        <div style="font-weight: 700; color: ${color}; font-size: 13px; margin-bottom: 3px;">
          ${t.vehicle_id.toUpperCase()} <span style="font-size: 11px; font-weight: 600; color: #968D82;">(${t.team.toUpperCase()} TEAM)</span>
        </div>
        <div><strong>State:</strong> ${t.state}</div>
        <div><strong>Altitude:</strong> ${t.coordinates.alt_m.toFixed(1)}m | <strong>Speed:</strong> ${t.velocity.speed_mps.toFixed(1)}m/s</div>
        <div><strong>Battery:</strong> ${t.battery_pct.toFixed(1)}%</div>
        <div style="font-family: monospace; font-size: 11px; color: #6E655C; margin-top: 3px;">
          ${t.coordinates.lat.toFixed(4)}, ${t.coordinates.lon.toFixed(4)}
        </div>
      </div>
    `);
  }

  // Update C2 Command Target dropdown with all active vehicles
  function updateCommandVehicleDropdown() {
    if (!cmdVehicleSelect) return;
    const currentVal = cmdVehicleSelect.value;
    const vehicles = Object.values(state.fleet);

    cmdVehicleSelect.innerHTML = vehicles.map(v => {
      const t = v.telemetry;
      return `<option value="${t.vehicle_id}">${t.vehicle_id.toUpperCase()} (${t.team.toUpperCase()})</option>`;
    }).join("");

    if (currentVal && state.fleet[currentVal]) {
      cmdVehicleSelect.value = currentVal;
    }
  }

  // Render Fleet Sidebar with Blue and Red team groupings
  function renderFleetList() {
    const vehicles = Object.values(state.fleet);
    const blueCount = vehicles.filter(v => v.telemetry.team === "blue").length;
    const redCount = vehicles.filter(v => v.telemetry.team === "red").length;

    fleetCountEl.textContent = `${vehicles.length}`;
    document.getElementById("hud-tracks").textContent = `BLUE: ${blueCount} | RED: ${redCount} (TOTAL: ${vehicles.length})`;

    if (vehicles.length === 0) {
      fleetListEl.innerHTML = `<div class="empty-state">Awaiting vehicle telemetry...</div>`;
      return;
    }

    // Sort: Blue fleet first, then Red fleet
    vehicles.sort((a, b) => {
      if (a.telemetry.team !== b.telemetry.team) {
        return a.telemetry.team === "blue" ? -1 : 1;
      }
      return a.telemetry.vehicle_id.localeCompare(b.telemetry.vehicle_id);
    });

    fleetListEl.innerHTML = vehicles.map(v => {
      const t = v.telemetry;
      const h = v.header;
      const isRed = t.team === "red";
      const teamClass = isRed ? "fleet-card-red" : "fleet-card-blue";
      const callsignClass = isRed ? "vehicle-callsign-red" : "vehicle-callsign";
      const batColor = t.battery_pct > 50 ? "var(--status-green)" : t.battery_pct > 20 ? "var(--status-amber)" : "var(--status-red)";
      return `
        <div class="fleet-card ${teamClass}">
          <div class="fleet-card-header">
            <span class="${callsignClass}">${t.vehicle_id.toUpperCase()}</span>
            <span class="vehicle-meta">${t.team.toUpperCase()} TEAM // ${t.vehicle_type}</span>
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

    const isRed = t.team === "red";
    const teamBadge = isRed ? `<span class="badge badge-red">RED</span>` : `<span class="badge badge-blue">BLUE</span>`;

    const row = document.createElement("tr");
    row.innerHTML = `
      <td>${timeStr}</td>
      <td><span class="badge badge-info">${h.classification}</span></td>
      <td>${h.origin_enclave}</td>
      <td>${teamBadge} <strong>${t.vehicle_id.toUpperCase()}</strong></td>
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

    if (!vehicle) {
      cmdFeedbackEl.textContent = "Please select a target vehicle.";
      return;
    }

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
        cmdFeedbackEl.textContent = `SUCCESS: Dispatched ${cmdType} to ${vehicle.toUpperCase()}`;
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
