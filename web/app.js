// Blue Polar Bear Tactical COP JavaScript

(function() {
  const state = {
    fleet: {},
    map: null,
    markers: {},
    polylines: {},
    ws: null,
    enclave: "TIER-1: PUBLIC",
    fleetFilter: "all",
    streamPaused: false,
    lastLogOverall: 0,
    lastVehicleState: {}
  };

  // DOM Elements
  const fleetListEl = document.getElementById("fleet-list");
  const fleetCountEl = document.getElementById("fleet-count");
  const wsStatusEl = document.getElementById("ws-status");
  const telemetryBodyEl = document.getElementById("telemetry-log-body");
  const dlqBodyEl = document.getElementById("dlq-log-body");
  const cmdFeedbackEl = document.getElementById("command-feedback");
  const cmdVehicleSelect = document.getElementById("cmd-vehicle");
  const backhaulStatusEl = document.getElementById("backhaul-status");
  const spoolStatusEl = document.getElementById("spool-status");
  const hudBackhaulEl = document.getElementById("hud-backhaul");
  const btnToggleDDIL = document.getElementById("btn-toggle-ddil");
  const btnRestoreLink = document.getElementById("btn-restore-link");
  const muleCloudTargetEl = document.getElementById("mule-cloud-target");
  const muleLatencyEl = document.getElementById("mule-latency");
  const muleSyncedEl = document.getElementById("mule-synced");
  const muleSpoolBytesEl = document.getElementById("mule-spool-bytes");
  const btnToggleStream = document.getElementById("btn-toggle-stream");
  const btnClearStream = document.getElementById("btn-clear-stream");
  const streamRateBadge = document.getElementById("stream-rate-badge");

  // Telemetry stream controls (Pause / Resume & Clear)
  if (btnToggleStream) {
    btnToggleStream.addEventListener("click", () => {
      state.streamPaused = !state.streamPaused;
      if (state.streamPaused) {
        btnToggleStream.textContent = "▶ RESUME";
        btnToggleStream.classList.add("active-paused");
        if (streamRateBadge) {
          streamRateBadge.textContent = "PAUSED";
          streamRateBadge.style.color = "var(--status-amber)";
        }
      } else {
        btnToggleStream.textContent = "⏸ PAUSE";
        btnToggleStream.classList.remove("active-paused");
        if (streamRateBadge) {
          streamRateBadge.textContent = "CALM (1 Hz)";
          streamRateBadge.style.color = "var(--accent-orange)";
        }
      }
    });
  }

  if (btnClearStream) {
    btnClearStream.addEventListener("click", () => {
      if (telemetryBodyEl) {
        telemetryBodyEl.innerHTML = `<tr><td colspan="9" class="empty-cell">Ingress log cleared. Awaiting new telemetry...</td></tr>`;
      }
    });
  }

  // Fleet filter switching
  document.querySelectorAll(".filter-btn").forEach(btn => {
    btn.addEventListener("click", () => {
      document.querySelectorAll(".filter-btn").forEach(b => b.classList.remove("active"));
      btn.classList.add("active");
      state.fleetFilter = btn.dataset.filter || "all";
      renderFleetList();
      syncMarkerVisibility();
    });
  });

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

  // Initialize Map with DoD & ATAK standard Esri Satellite Recon & Tactical Topo
  function initMap() {
    if (typeof L !== "undefined") {
      try {
        const esriSatellite = L.tileLayer("https://server.arcgisonline.com/ArcGIS/rest/services/World_Imagery/MapServer/tile/{z}/{y}/{x}", {
          attribution: 'Tiles &copy; Esri &mdash; Source: Esri, Maxar, Earthstar Geographics, USDA, USGS',
          maxZoom: 19
        });

        const esriTopo = L.tileLayer("https://server.arcgisonline.com/ArcGIS/rest/services/World_Topo_Map/MapServer/tile/{z}/{y}/{x}", {
          attribution: 'Tiles &copy; Esri &mdash; USGS, DeLorme, TomTom, FAO, NPS',
          maxZoom: 19
        });

        state.map = L.map("map", {
          center: [31.6500, -8.0100],
          zoom: 12,
          layers: [esriSatellite]
        });

        const baseMaps = {
          "Satellite Recon (Esri)": esriSatellite,
          "Tactical Topo (Esri)": esriTopo
        };

        L.control.layers(baseMaps, null, { position: "topright" }).addTo(state.map);
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
      if (state.fleetFilter !== "all" && v.telemetry.team !== state.fleetFilter) {
        return;
      }
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
        } else if (envelope && envelope.type === "backhaul_status" && envelope.metrics) {
          updateBackhaulUI(envelope.metrics);
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
    const isLowBatt = t.battery_pct < 20;

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

    const battDisplay = isLowBatt 
      ? `<span style="color:#BA4540; font-weight:700;">${t.battery_pct.toFixed(1)}% [CRITICAL LOW]</span>`
      : `${t.battery_pct.toFixed(1)}%`;

    state.markers[vID].bindPopup(`
      <div style="font-family: -apple-system, BlinkMacSystemFont, sans-serif; color: #2B2621; font-size: 12px; line-height: 1.5; padding: 2px;">
        <div style="font-weight: 700; color: ${color}; font-size: 13px; margin-bottom: 3px;">
          ${t.vehicle_id.toUpperCase()} <span style="font-size: 11px; font-weight: 600; color: #968D82;">(${t.team.toUpperCase()} TEAM)</span>
        </div>
        <div><strong>State:</strong> ${t.state}</div>
        <div><strong>Altitude:</strong> ${t.coordinates.alt_m.toFixed(1)}m | <strong>Speed:</strong> ${t.velocity.speed_mps.toFixed(1)}m/s</div>
        <div><strong>Battery:</strong> ${battDisplay}</div>
        <div style="font-family: monospace; font-size: 11px; color: #6E655C; margin-top: 3px;">
          ${t.coordinates.lat.toFixed(4)}, ${t.coordinates.lon.toFixed(4)}
        </div>
      </div>
    `);

    // Synchronize visibility with active filter
    syncMarkerVisibility();
  }

  // Synchronize Leaflet map markers and polylines with the active fleet filter
  function syncMarkerVisibility() {
    if (!state.map) return;
    Object.entries(state.markers).forEach(([vID, marker]) => {
      const v = state.fleet[vID];
      if (!v) return;
      const isMatch = state.fleetFilter === "all" || v.telemetry.team === state.fleetFilter;
      if (isMatch) {
        if (!state.map.hasLayer(marker)) state.map.addLayer(marker);
        if (state.polylines[vID] && !state.map.hasLayer(state.polylines[vID])) {
          state.map.addLayer(state.polylines[vID]);
        }
      } else {
        if (state.map.hasLayer(marker)) state.map.removeLayer(marker);
        if (state.polylines[vID] && state.map.hasLayer(state.polylines[vID])) {
          state.map.removeLayer(state.polylines[vID]);
        }
      }
    });
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

  // Render Fleet Sidebar with Blue and Red team groupings, filter, and low-battery alerts
  function renderFleetList() {
    const vehicles = Object.values(state.fleet);
    const blueCount = vehicles.filter(v => v.telemetry.team === "blue").length;
    const redCount = vehicles.filter(v => v.telemetry.team === "red").length;

    fleetCountEl.textContent = `${vehicles.length}`;
    document.getElementById("hud-tracks").textContent = `BLUE: ${blueCount} | RED: ${redCount} (TOTAL: ${vehicles.length})`;

    // Update filter button counts
    const btnAll = document.querySelector('.filter-btn[data-filter="all"]');
    const btnBlue = document.querySelector('.filter-btn[data-filter="blue"]');
    const btnRed = document.querySelector('.filter-btn[data-filter="red"]');
    if (btnAll) btnAll.textContent = `ALL (${vehicles.length})`;
    if (btnBlue) btnBlue.textContent = `BLUE (${blueCount})`;
    if (btnRed) btnRed.textContent = `RED (${redCount})`;

    if (vehicles.length === 0) {
      fleetListEl.innerHTML = `<div class="empty-state">Awaiting vehicle telemetry...</div>`;
      return;
    }

    // Filter displayed vehicles
    const displayedVehicles = vehicles.filter(v => {
      if (state.fleetFilter === "blue") return v.telemetry.team === "blue";
      if (state.fleetFilter === "red") return v.telemetry.team === "red";
      return true;
    });

    if (displayedVehicles.length === 0) {
      fleetListEl.innerHTML = `<div class="empty-state">No vehicles matching '${state.fleetFilter.toUpperCase()}' filter.</div>`;
      return;
    }

    // Sort: Blue fleet first, then Red fleet
    displayedVehicles.sort((a, b) => {
      if (a.telemetry.team !== b.telemetry.team) {
        return a.telemetry.team === "blue" ? -1 : 1;
      }
      return a.telemetry.vehicle_id.localeCompare(b.telemetry.vehicle_id);
    });

    fleetListEl.innerHTML = displayedVehicles.map(v => {
      const t = v.telemetry;
      const h = v.header;
      const isRed = t.team === "red";
      const isLowBatt = t.battery_pct < 20;
      const teamClass = isRed ? "fleet-card-red" : "fleet-card-blue";
      const lowBattClass = isLowBatt ? "low-battery" : "";
      const callsignClass = isRed ? "vehicle-callsign-red" : "vehicle-callsign";
      const batColor = t.battery_pct > 50 ? "var(--status-green)" : t.battery_pct > 20 ? "var(--status-amber)" : "var(--status-red)";
      const lowBattBadge = isLowBatt ? `<span class="badge badge-low-battery">LOW BATT (${t.battery_pct.toFixed(0)}%)</span>` : "";

      return `
        <div class="fleet-card ${teamClass} ${lowBattClass}">
          <div class="fleet-card-header">
            <div style="display:flex; align-items:center; gap:6px;">
              <span class="${callsignClass}">${t.vehicle_id.toUpperCase()}</span>
              ${lowBattBadge}
            </div>
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

  // Append Telemetry Log Row with Smart Calming & Rate Throttling
  function appendTelemetryRow(env) {
    if (state.streamPaused || !telemetryBodyEl) return;

    const t = env.telemetry;
    const h = env.header;
    const vID = t.vehicle_id;
    const now = Date.now();

    // Check if this is a high-priority event (State Transition or Low Battery)
    const prevState = state.lastVehicleState[vID];
    const isStateChange = prevState && prevState !== t.state;
    state.lastVehicleState[vID] = t.state;
    const isLowBatt = t.battery_pct < 20;

    // Routine cruise telemetry is throttled to a calm 1 Hz rate limit overall
    if (!isStateChange && !isLowBatt) {
      if (now - state.lastLogOverall < 1000) {
        return;
      }
    }
    state.lastLogOverall = now;

    const timeStr = new Date().toLocaleTimeString();

    if (telemetryBodyEl.querySelector(".empty-cell")) {
      telemetryBodyEl.innerHTML = "";
    }

    const isRed = t.team === "red";
    const teamBadge = isRed ? `<span class="badge badge-red">RED</span>` : `<span class="badge badge-blue">BLUE</span>`;

    let stateBadge = t.state;
    if (isStateChange) {
      stateBadge = `<span class="badge badge-warning" title="State Transitioned">${t.state}</span>`;
    }

    let battBadge = `${t.battery_pct.toFixed(1)}%`;
    if (isLowBatt) {
      battBadge = `<span class="badge badge-danger">${t.battery_pct.toFixed(0)}%</span>`;
    }

    const row = document.createElement("tr");
    row.innerHTML = `
      <td>${timeStr}</td>
      <td><span class="badge badge-info">${h.classification}</span></td>
      <td>${h.origin_enclave}</td>
      <td>${teamBadge} <strong>${t.vehicle_id.toUpperCase()}</strong></td>
      <td>${stateBadge}</td>
      <td>${battBadge}</td>
      <td>${t.coordinates.lat.toFixed(2)}, ${t.coordinates.lon.toFixed(2)}</td>
      <td>${t.coordinates.alt_m.toFixed(0)}m</td>
      <td title="${h.digest}">${h.digest.substring(0, 8)}...</td>
    `;

    telemetryBodyEl.prepend(row);
    while (telemetryBodyEl.children.length > 25) {
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

  // Update Starlink Backhaul & Data Mule UI
  function updateBackhaulUI(m) {
    if (!m) return;
    if (m.status === "ONLINE") {
      if (backhaulStatusEl) {
        backhaulStatusEl.textContent = "ONLINE (STARLINK)";
        backhaulStatusEl.className = "badge badge-success";
      }
      if (hudBackhaulEl) {
        hudBackhaulEl.textContent = `ONLINE (${m.latency_ms}ms)`;
        hudBackhaulEl.style.color = "var(--status-green)";
      }
      if (btnToggleDDIL) btnToggleDDIL.style.display = "block";
      if (btnRestoreLink) btnRestoreLink.style.display = "none";
    } else {
      if (backhaulStatusEl) {
        backhaulStatusEl.textContent = "DDIL BUFFERING";
        backhaulStatusEl.className = "badge badge-warning";
      }
      if (hudBackhaulEl) {
        hudBackhaulEl.textContent = "DDIL OUTAGE (BUFFERING)";
        hudBackhaulEl.style.color = "var(--status-amber)";
      }
      if (btnToggleDDIL) btnToggleDDIL.style.display = "none";
      if (btnRestoreLink) btnRestoreLink.style.display = "block";
    }

    if (spoolStatusEl) {
      spoolStatusEl.textContent = `${m.spooled_packets} PACKETS`;
      spoolStatusEl.className = m.spooled_packets > 0 ? "badge badge-warning" : "badge badge-info";
    }
    if (muleCloudTargetEl) muleCloudTargetEl.textContent = m.cloud_target || "relay.platformstaq.com";
    if (muleLatencyEl) muleLatencyEl.textContent = `${m.latency_ms} ms`;
    if (muleSyncedEl) muleSyncedEl.textContent = `${m.synced_total}`;
    if (muleSpoolBytesEl) {
      const bytes = m.spooled_bytes || 0;
      if (bytes < 1024) {
        muleSpoolBytesEl.textContent = `${bytes} B`;
      } else {
        muleSpoolBytesEl.textContent = `${(bytes / 1024).toFixed(1)} KB`;
      }
    }
  }

  // Simulate Backhaul DDIL Disconnect / Reconnect
  async function simulateBackhaul(ddilActive) {
    try {
      const resp = await fetch("/api/v1/backhaul/simulate", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ ddil_active: ddilActive })
      });
      if (resp.ok) {
        const metrics = await resp.json();
        updateBackhaulUI(metrics);
        cmdFeedbackEl.textContent = ddilActive
          ? "TACTICAL DDIL ACTIVE: Satellite backhaul dropped. Telemetry is buffering to local Data Mule."
          : "SATELLITE BACKHAUL RESTORED: Tactical Data Mule flushed spooled packets upstream.";
      }
    } catch (e) {
      console.warn("Backhaul simulation error:", e);
    }
  }

  if (btnToggleDDIL) {
    btnToggleDDIL.addEventListener("click", () => simulateBackhaul(true));
  }
  if (btnRestoreLink) {
    btnRestoreLink.addEventListener("click", () => simulateBackhaul(false));
  }

  // Fallback Polling for Backhaul Status
  async function pollBackhaul() {
    try {
      const resp = await fetch("/api/v1/backhaul");
      if (resp.ok) {
        const metrics = await resp.json();
        updateBackhaulUI(metrics);
      }
    } catch (e) {
      // Ignore during initial boot
    }
    setTimeout(pollBackhaul, 3000);
  }

  // Initialization
  window.addEventListener("DOMContentLoaded", () => {
    initMap();
    connectWebSocket();
    pollDLQ();
    pollBackhaul();
  });
})();
