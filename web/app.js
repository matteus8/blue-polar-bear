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
    lastVehicleState: {},
    selectedVehicleId: "blue-alpha",
    lastDropdownIDs: ""
  };

  // DOM Elements
  const fleetListEl = document.getElementById("fleet-list");
  const fleetCountEl = document.getElementById("fleet-count");
  const wsStatusEl = document.getElementById("ws-status");
  const telemetryBodyEl = document.getElementById("telemetry-log-body");
  const dlqBodyEl = document.getElementById("dlq-log-body");
  const cmdFeedbackEl = document.getElementById("command-feedback");
  const injectorFeedbackEl = document.getElementById("injector-feedback");
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

  // Tab switching helper
  function switchToTab(tabName) {
    document.querySelectorAll(".tab-btn").forEach(b => {
      if (b.dataset.tab === tabName) {
        b.classList.add("active");
      } else {
        b.classList.remove("active");
      }
    });
    document.querySelectorAll(".tab-content").forEach(c => {
      if (c.id === `tab-${tabName}`) {
        c.classList.add("active");
      } else {
        c.classList.remove("active");
      }
    });
  }

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
      switchToTab(btn.dataset.tab);
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
    const existing = state.fleet[vID];

    // Monotonic sequence freshness guard:
    // Drop replayed or out-of-order packets (e.g., historical packets during mule reconnect)
    // so vehicle markers don't jump backward or jitter.
    if (existing && existing.telemetry) {
      const isStale = t.sequence <= existing.telemetry.sequence;
      // Allow sequence reset if vehicle hasn't been heard from for >10s (agent restart)
      const isRestart = (existing.telemetry.sequence - t.sequence > 50) && ((Date.now() - (existing.lastUpdate || 0)) > 10000);
      if (isStale && !isRestart) {
        return;
      }
    }

    const prevTeam = existing ? existing.telemetry.team : null;

    if (!existing) {
      state.fleet[vID] = {
        telemetry: t,
        header: env.header,
        tracks: [],
        lastUpdate: Date.now()
      };
      updateCommandVehicleDropdown();
    } else {
      existing.telemetry = t;
      existing.header = env.header;
      existing.lastUpdate = Date.now();
      if (prevTeam !== t.team) {
        updateCommandVehicleDropdown();
      }
    }

    // Dynamic Enclave Header & HUD Synchronization
    if (env.header && env.header.classification) {
      const cls = env.header.classification;
      if (cls !== state.enclave) {
        state.enclave = cls;
        const bannerEl = document.getElementById("banner-classification");
        const hudEnclaveEl = document.getElementById("hud-enclave");
        const hudCoarsenEl = document.getElementById("hud-coarsening");
        
        if (cls === "TIER-2: RESTRICTED") {
          if (bannerEl) {
            bannerEl.className = "classification-banner tier-restricted";
            const txt = bannerEl.querySelector(".banner-text");
            if (txt) txt.textContent = "// SIMULATION ONLY // TIER-2: RESTRICTED // TACTICAL ENCLAVE //";
          }
          if (hudEnclaveEl) hudEnclaveEl.textContent = "TIER-2: RESTRICTED";
          if (hudCoarsenEl) hudCoarsenEl.textContent = "TACTICAL HIGH-RES (6 DECIMALS ~0.1m)";
        } else if (cls === "TIER-1: PUBLIC") {
          if (bannerEl) {
            bannerEl.className = "classification-banner tier-public";
            const txt = bannerEl.querySelector(".banner-text");
            if (txt) txt.textContent = "// SIMULATION ONLY // TIER-1: PUBLIC // SYNTHETIC ENCLAVE //";
          }
          if (hudEnclaveEl) hudEnclaveEl.textContent = "TIER-1: PUBLIC";
          if (hudCoarsenEl) hudCoarsenEl.textContent = "2 DECIMALS (~1.1 km)";
        }
      }
    }

    // Add track point only if vehicle actually moved (avoid duplicate zero-distance points)
    const coord = [t.coordinates.lat, t.coordinates.lon];
    const tracks = state.fleet[vID].tracks;
    if (tracks.length === 0 || 
        Math.abs(tracks[tracks.length - 1][0] - coord[0]) > 0.00005 || 
        Math.abs(tracks[tracks.length - 1][1] - coord[1]) > 0.00005) {
      tracks.push(coord);
      if (tracks.length > 30) {
        tracks.shift();
      }
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

  // Update Leaflet marker and breadcrumb track with Team colors & Tactical Badges
  function updateMapMarker(vID, coord, t) {
    if (!state.map) return;

    const isRed = t.team === "red";
    const color = isRed ? "#BA4540" : "#326B94";
    const isLowBatt = t.battery_pct < 20;
    const isSelected = state.selectedVehicleId === vID;

    let stateClass = "";
    if (t.state === "RTB") stateClass = "state-rtb";
    else if (t.state === "LANDED") stateClass = "state-landed";

    const shortID = t.vehicle_id.replace("blue-", "B-").replace("red-", "R-").toUpperCase();
    const markerHTML = `
      <div class="marker-container ${isSelected ? 'selected-target' : ''} ${stateClass}">
        <div class="marker-dot" style="background:${color};"></div>
        <div class="marker-label">${shortID} <span class="marker-sub">[${t.state}]</span></div>
      </div>
    `;

    if (!state.markers[vID]) {
      const icon = L.divIcon({
        className: 'vehicle-marker',
        html: markerHTML,
        iconSize: [60, 36],
        iconAnchor: [30, 8]
      });
      const marker = L.marker(coord, { icon }).addTo(state.map);
      marker.on("click", () => {
        selectVehicle(vID, false);
      });
      state.markers[vID] = marker;
      state.polylines[vID] = L.polyline(state.fleet[vID].tracks, {
        color: color,
        weight: 2.5,
        opacity: 0.7,
        dashArray: '4, 4'
      }).addTo(state.map);
    } else {
      state.markers[vID].setLatLng(coord);
      state.polylines[vID].setLatLngs(state.fleet[vID].tracks);
      const icon = L.divIcon({
        className: 'vehicle-marker',
        html: markerHTML,
        iconSize: [60, 36],
        iconAnchor: [30, 8]
      });
      state.markers[vID].setIcon(icon);
    }

    const battDisplay = isLowBatt 
      ? `<span style="color:#BA4540; font-weight:700;">${t.battery_pct.toFixed(1)}% [CRITICAL LOW]</span>`
      : `${t.battery_pct.toFixed(1)}%`;

    state.markers[vID].bindPopup(`
      <div style="font-family: -apple-system, BlinkMacSystemFont, sans-serif; color: #2B2621; font-size: 12px; line-height: 1.5; padding: 2px;">
        <div style="font-weight: 700; color: ${color}; font-size: 13px; margin-bottom: 3px;">
          ${t.vehicle_id.toUpperCase()} <span style="font-size: 11px; font-weight: 600; color: #968D82;">(${t.team.toUpperCase()} TEAM)</span>
        </div>
        <div><strong>State:</strong> <span style="font-weight:700; color:${t.state === 'RTB' ? '#D9822B' : color};">${t.state}</span></div>
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

  // Update C2 Command Target dropdown with friendly blue vehicles only (stable, preserves selection)
  function updateCommandVehicleDropdown() {
    if (!cmdVehicleSelect) return;
    const currentVal = cmdVehicleSelect.value || state.selectedVehicleId;
    const blueVehicles = Object.values(state.fleet).filter(v => v.telemetry && v.telemetry.team === "blue");
    if (blueVehicles.length === 0) {
      cmdVehicleSelect.innerHTML = `<option value="">No friendly assets available</option>`;
      cmdVehicleSelect.disabled = true;
      return;
    }

    const newIDs = blueVehicles.map(v => v.telemetry.vehicle_id).sort().join(",");
    if (state.lastDropdownIDs === newIDs) {
      return;
    }
    state.lastDropdownIDs = newIDs;

    cmdVehicleSelect.disabled = false;
    cmdVehicleSelect.innerHTML = blueVehicles.map(v => {
      const t = v.telemetry;
      const isSel = (t.vehicle_id === currentVal) ? "selected" : "";
      return `<option value="${t.vehicle_id}" ${isSel}>${t.vehicle_id.toUpperCase()} (BLUE)</option>`;
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

    const filtered = vehicles.filter(v => {
      if (state.fleetFilter === "all") return true;
      return v.telemetry.team === state.fleetFilter;
    });

    if (filtered.length === 0) {
      fleetListEl.innerHTML = `<div class="empty-state">No vehicles in ${state.fleetFilter.toUpperCase()} filter.</div>`;
      return;
    }

    // Sort: Blue fleet first, then alphabetically
    const displayedVehicles = [...filtered].sort((a, b) => {
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

      const isSelected = state.selectedVehicleId === t.vehicle_id;
      const selectedClass = isSelected ? "selected-card" : "";

      return `
        <div class="fleet-card ${teamClass} ${lowBattClass} ${selectedClass}" data-vehicle-id="${t.vehicle_id}" role="button" tabindex="0" aria-label="Select and inspect ${t.vehicle_id.toUpperCase()}">
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

    fleetListEl.querySelectorAll(".fleet-card").forEach(card => {
      card.addEventListener("click", () => selectVehicle(card.dataset.vehicleId, true));
      card.addEventListener("keydown", (e) => {
        if (e.key === "Enter" || e.key === " ") {
          e.preventDefault();
          selectVehicle(card.dataset.vehicleId, true);
        }
      });
    });
  }

  // Select a vehicle, sync C2 dropdown, highlight card, and apply tactical target reticle
  function selectVehicle(vID, shouldPan = false) {
    if (!vID) return;
    state.selectedVehicleId = vID;

    // Sync C2 dropdown if this is a friendly blue drone
    if (cmdVehicleSelect && state.fleet[vID] && state.fleet[vID].telemetry?.team === "blue") {
      cmdVehicleSelect.value = vID;
    }

    // Highlight card in fleet list
    if (fleetListEl) {
      fleetListEl.querySelectorAll(".fleet-card").forEach(card => {
        if (card.dataset.vehicleId === vID) {
          card.classList.add("selected-card");
        } else {
          card.classList.remove("selected-card");
        }
      });
    }

    // Update marker styling and open popup
    Object.keys(state.markers).forEach(id => {
      const marker = state.markers[id];
      const el = marker.getElement ? marker.getElement() : null;
      if (el) {
        const container = el.querySelector(".marker-container");
        if (container) {
          if (id === vID) {
            container.classList.add("selected-target");
          } else {
            container.classList.remove("selected-target");
          }
        }
      }
    });

    if (state.markers[vID]) {
      state.markers[vID].openPopup();
      if (shouldPan && state.map && state.fleet[vID]) {
        state.map.panTo([state.fleet[vID].telemetry.coordinates.lat, state.fleet[vID].telemetry.coordinates.lon], { animate: true });
      }
    }
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
      <td>${t.coordinates.lat.toFixed(4)}, ${t.coordinates.lon.toFixed(4)}</td>
      <td>${t.coordinates.alt_m.toFixed(0)}m</td>
      <td title="${h.digest}">${h.digest.substring(0, 8)}...</td>
    `;

    telemetryBodyEl.prepend(row);
    while (telemetryBodyEl.children.length > 25) {
      telemetryBodyEl.removeChild(telemetryBodyEl.lastChild);
    }
  }

  // Poll DLQ Records from CDS Guard via C2 Gateway proxy
  async function fetchDLQ() {
    try {
      const resp = await fetch("/api/v1/dlq");
      if (resp.ok) {
        const records = await resp.json();
        if (records && records.length > 0) {
          renderDLQTable(records);
        }
      } else if (resp.status === 503) {
        if (dlqBodyEl && !dlqBodyEl.querySelector(".dlq-status-offline")) {
          const existingRows = dlqBodyEl.querySelectorAll("tr:not(.empty-cell)");
          if (existingRows.length === 0) {
            dlqBodyEl.innerHTML = `<tr><td colspan="7" class="empty-cell dlq-status-offline" style="color:var(--status-amber);">CDS Guard offline or unreachable (503 Service Unavailable).</td></tr>`;
          }
        }
      }
    } catch (e) {
      // CDS HTTP might be on another port or host in test mode
    }
  }

  function pollDLQNow() {
    fetchDLQ();
  }

  function pollDLQ() {
    fetchDLQ();
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

  // Synchronize dropdown changes with active map and fleet list selection
  if (cmdVehicleSelect) {
    cmdVehicleSelect.addEventListener("change", () => {
      selectVehicle(cmdVehicleSelect.value, false);
    });
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

      if (!resp.ok) {
        let errText = "Unknown error";
        try {
          const data = await resp.json();
          errText = data.error || data.message || JSON.stringify(data);
        } catch {
          errText = await resp.text();
        }
        cmdFeedbackEl.textContent = `FAILED: ${errText}`;
        cmdFeedbackEl.style.color = "var(--status-red)";
        return;
      }

      const data = await resp.json();
      cmdFeedbackEl.textContent = `SUCCESS: Dispatched ${cmdType} to ${vehicle.toUpperCase()}`;
      cmdFeedbackEl.style.color = "var(--status-green)";
    } catch (err) {
      cmdFeedbackEl.textContent = `NETWORK ERROR: ${err.message}`;
      cmdFeedbackEl.style.color = "var(--status-red)";
    }
  });

  // Zero-Trust Test Injections (Demonstrates CDS Guard fail-closed behavior)
  document.getElementById("btn-inject-tier2").addEventListener("click", () => {
    injectSyntheticTelemetry("TIER-2: RESTRICTED", false, "Emitted TIER-2 Restricted -> Redacted & Down-tagged to TIER-1");
  });

  document.getElementById("btn-inject-tier3").addEventListener("click", () => {
    injectSyntheticTelemetry("TIER-3: CRITICAL", false, "Emitted TIER-3 Critical -> Barred from Egress (CDS Quarantined)");
  });

  document.getElementById("btn-inject-tamper").addEventListener("click", () => {
    injectSyntheticTelemetry("TIER-2: RESTRICTED", true, "Emitted Tampered Digest -> Cryptographic Breach (CDS Quarantined)");
  });

  async function injectSyntheticTelemetry(tier, tamper, note) {
    if (injectorFeedbackEl) {
      injectorFeedbackEl.textContent = `Injecting: ${note}...`;
      injectorFeedbackEl.style.color = "var(--status-amber)";
    }
    try {
      const resp = await fetch("/api/v1/inject", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          tier: tier,
          tamper: tamper,
          target_vehicle: "sim-probe-1"
        })
      });

      if (resp.ok) {
        if (injectorFeedbackEl) {
          injectorFeedbackEl.textContent = `SUCCESS: ${note}`;
          injectorFeedbackEl.style.color = (tamper || tier === "TIER-3: CRITICAL") ? "var(--status-red)" : "var(--status-green)";
        }
        if (tamper || tier === "TIER-3: CRITICAL") {
          switchToTab("cds-quarantine");
        }
        setTimeout(pollDLQNow, 300);
      } else {
        let errText = "Injection failed";
        try {
          const data = await resp.json();
          errText = data.error || data.message || JSON.stringify(data);
        } catch {
          errText = await resp.text();
        }
        if (injectorFeedbackEl) {
          injectorFeedbackEl.textContent = `FAILED: ${errText}`;
          injectorFeedbackEl.style.color = "var(--status-red)";
        }
      }
    } catch (err) {
      if (injectorFeedbackEl) {
        injectorFeedbackEl.textContent = `NETWORK ERROR: ${err.message}`;
        injectorFeedbackEl.style.color = "var(--status-red)";
      }
    }
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
