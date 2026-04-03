// ── State ────────────────────────────────────────
let session = null;
let ws = null;
let chart = null;
let chartData = [[], [], [], [], []]; // [timestamps, p1, p2, p3, p4]
let useCelsius = localStorage.getItem("useCelsius") === "true";
let userRole = 'viewer';
let canRegister = false;

const PROBE_COLORS_DARK  = ["#c0c0c0", "#4a90d9", "#888888", "#d4a017"];
const PROBE_COLORS_LIGHT = ["#909090", "#2a6fb5", "#333333", "#b8860b"];
const DISCONNECTED = -999.0;

function probeColors() {
  return document.documentElement.dataset.theme === "light" ? PROBE_COLORS_LIGHT : PROBE_COLORS_DARK;
}

// ── DOM refs ────────────────────────────────────
const probeGrid     = document.getElementById("probe-grid");
const chartEl       = document.getElementById("chart-container");
const toggleBtn     = document.getElementById("toggle-unit");
const themeBtn      = document.getElementById("toggle-theme");
const resetBtn      = document.getElementById("reset-cook");
const banner        = document.getElementById("reconnect-banner");
const modalOverlay  = document.getElementById("modal-overlay");
const modalForm     = document.getElementById("modal-form");
const modalTitle    = document.getElementById("modal-title");
const modalProbeId  = document.getElementById("modal-probe-id");
const modalLabel    = document.getElementById("modal-label");
const modalTarget   = document.getElementById("modal-target");
const modalHigh     = document.getElementById("modal-high");
const modalLow      = document.getElementById("modal-low");
const modalCancel   = document.getElementById("modal-cancel");
const batteryBadge  = document.getElementById("battery-badge");
const batteryText   = document.getElementById("battery-text");
const batteryFill   = document.getElementById("battery-fill");

// ── Helpers ─────────────────────────────────────
function toDisplay(f) {
  if (f === null || f === undefined || f === DISCONNECTED) return "---";
  const v = useCelsius ? (f - 32) * 5 / 9 : f;
  return Math.round(v) + "\u00B0" + (useCelsius ? "C" : "F");
}

function unitLabel() { return useCelsius ? "\u00B0C" : "\u00B0F"; }

function updateBattery(pct) {
  if (pct === null || pct === undefined) return;
  batteryText.textContent = pct + "%";
  if (batteryFill) batteryFill.setAttribute("width", Math.max(1, Math.round(14 * pct / 100)));
  batteryBadge.classList.remove("battery-low", "battery-mid", "battery-ok");
  if (pct <= 20) batteryBadge.classList.add("battery-low");
  else if (pct <= 50) batteryBadge.classList.add("battery-mid");
  else batteryBadge.classList.add("battery-ok");
}

function probeStatus(probe) {
  if (!probe.connected || probe.current_temp === DISCONNECTED) return "disconnected";
  const target = probe.alert && probe.alert.target_temp;
  if (target != null && target > 0) {
    if (probe.current_temp >= target) return "alert";
    if (target - probe.current_temp <= 10) return "warning";
  }
  const high = probe.alert && probe.alert.high_temp;
  if (high != null && high > 0 && probe.current_temp >= high) return "alert";
  const low = probe.alert && probe.alert.low_temp;
  if (low != null && low > 0 && probe.current_temp <= low) return "alert";
  return "normal";
}

// ── Probe Cards ─────────────────────────────────
function renderProbeCards() {
  if (!session || !session.probes) return;
  probeGrid.innerHTML = "";
  session.probes.forEach((p) => {
    const status = probeStatus(p);
    const card = document.createElement("div");
    card.className = "probe-card" + (status === "disconnected" ? " status-disconnected" : "");
    card.dataset.id = p.id;
    card.style.borderLeftColor = probeColors()[p.id - 1];

    const target = p.alert && p.alert.target_temp;
    const targetStr = (target != null && target > 0) ? "Target: " + toDisplay(target) : "";

    card.innerHTML =
      '<div class="probe-label">' + escHtml(p.label || "Probe " + p.id) + '</div>' +
      '<div class="probe-temp">' + toDisplay(p.current_temp) + '</div>' +
      (targetStr ? '<div class="probe-target">' + targetStr + '</div>' : '');

    card.addEventListener("click", () => { if (userRole !== 'admin') return; openModal(p); });
    probeGrid.appendChild(card);
  });
}

function escHtml(s) {
  const d = document.createElement("div");
  d.textContent = s;
  return d.innerHTML;
}

function updateProbeFromReading(temps) {
  if (!session || !session.probes) return;
  temps.forEach((t, i) => {
    if (i < session.probes.length) {
      session.probes[i].current_temp = t;
      session.probes[i].connected = (t !== DISCONNECTED);
    }
  });
  renderProbeCards();
}

// ── Modal ───────────────────────────────────────
function openModal(probe) {
  modalProbeId.value = probe.id;
  modalTitle.textContent = "Edit " + (probe.label || "Probe " + probe.id);
  modalLabel.value = probe.label || "";
  modalTarget.value = (probe.alert && probe.alert.target_temp) || "";
  modalHigh.value   = (probe.alert && probe.alert.high_temp)   || "";
  modalLow.value    = (probe.alert && probe.alert.low_temp)    || "";
  modalOverlay.classList.remove("hidden");
}

function closeModal() { modalOverlay.classList.add("hidden"); }

modalCancel.addEventListener("click", closeModal);
modalOverlay.addEventListener("click", (e) => {
  if (e.target === modalOverlay) closeModal();
});

modalForm.addEventListener("submit", async (e) => {
  e.preventDefault();
  const id = parseInt(modalProbeId.value, 10);

  const alertPayload = {
    probe_id: id,
    alert: {
      target_temp: parseFloat(modalTarget.value) || null,
      high_temp:   parseFloat(modalHigh.value)   || null,
      low_temp:    parseFloat(modalLow.value)     || null,
    }
  };

  try {
    await fetch("/api/alerts", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(alertPayload),
    });
  } catch (err) {
    console.error("Failed to save alert:", err);
  }

  // Update label locally
  const probe = session && session.probes && session.probes.find(p => p.id === id);
  if (probe) {
    probe.label = modalLabel.value || "Probe " + id;
    if (!probe.alert) probe.alert = {};
    probe.alert.target_temp = alertPayload.alert.target_temp;
    probe.alert.high_temp   = alertPayload.alert.high_temp;
    probe.alert.low_temp    = alertPayload.alert.low_temp;
  }

  renderProbeCards();
  closeModal();
});

// ── Theme Toggle ────────────────────────────────
let darkMode = localStorage.getItem("theme") !== "light";

function applyTheme() {
  document.documentElement.dataset.theme = darkMode ? "dark" : "light";
  themeBtn.textContent = darkMode ? "Dark" : "Light";
}
applyTheme();

themeBtn.addEventListener("click", () => {
  darkMode = !darkMode;
  localStorage.setItem("theme", darkMode ? "dark" : "light");
  applyTheme();
  renderProbeCards();
  if (chart) rebuildChart();
});

// ── F/C Toggle ──────────────────────────────────
function updateUnitBtn() { toggleBtn.textContent = useCelsius ? "\u00B0C" : "\u00B0F"; }
updateUnitBtn();

toggleBtn.addEventListener("click", () => {
  useCelsius = !useCelsius;
  localStorage.setItem("useCelsius", useCelsius);
  updateUnitBtn();
  renderProbeCards();
  if (chart) rebuildChart();
});

// ── Reset Cook ──────────────────────────────────
resetBtn.addEventListener("click", async () => {
  if (!confirm("Reset cook session? This clears all temperature history.")) return;
  try {
    await fetch("/api/session/reset", { method: "POST" });
    chartData = [[], [], [], [], []];
    if (chart) { chart.destroy(); chart = null; }
    await loadSession();
  } catch (err) {
    console.error("Reset failed:", err);
  }
});

// ── Chart ───────────────────────────────────────
function chartOpts() {
  const width = Math.min(chartEl.clientWidth - 32, 920);
  const style = getComputedStyle(document.documentElement);
  const axisStroke = style.getPropertyValue("--axis-stroke").trim();
  const gridStroke = style.getPropertyValue("--grid-line").trim();
  const colors = probeColors();
  return {
    width: width,
    height: 280,
    title: "Temperature History",
    cursor: { show: true },
    scales: { x: { time: true }, y: {} },
    axes: [
      { stroke: axisStroke, grid: { stroke: gridStroke } },
      {
        stroke: axisStroke,
        grid: { stroke: gridStroke },
        values: (_, ticks) => ticks.map(v => {
          const d = useCelsius ? (v - 32) * 5 / 9 : v;
          return Math.round(d) + "\u00B0" + (useCelsius ? "C" : "F");
        }),
      },
    ],
    series: [
      {},
      { label: "Probe 1", stroke: colors[0], width: 2 },
      { label: "Probe 2", stroke: colors[1], width: 2 },
      { label: "Probe 3", stroke: colors[2], width: 2 },
      { label: "Probe 4", stroke: colors[3], width: 2 },
    ],
  };
}

function initChart() {
  if (chart) chart.destroy();
  if (chartData[0].length === 0) {
    // show empty state
    chart = new uPlot(chartOpts(), chartData, chartEl);
    return;
  }
  chart = new uPlot(chartOpts(), chartData, chartEl);
}

function rebuildChart() {
  if (chart) chart.destroy();
  chart = new uPlot(chartOpts(), chartData, chartEl);
}

function appendChartPoint(ts, temps) {
  chartData[0].push(ts);
  for (let i = 0; i < 4; i++) {
    const v = (temps[i] !== undefined && temps[i] !== DISCONNECTED) ? temps[i] : null;
    chartData[i + 1].push(v);
  }
  if (chart) chart.setData(chartData);
}

// Responsive resize
window.addEventListener("resize", () => {
  if (chart) rebuildChart();
});

// ── Load session ────────────────────────────────
async function loadSession() {
  try {
    const res = await fetch("/api/session");
    session = await res.json();
  } catch (err) {
    console.error("Failed to load session:", err);
    // Create a default session so the UI still renders
    session = {
      probes: [
        { id: 1, label: "Probe 1", current_temp: DISCONNECTED, connected: false, alert: {} },
        { id: 2, label: "Probe 2", current_temp: DISCONNECTED, connected: false, alert: {} },
        { id: 3, label: "Probe 3", current_temp: DISCONNECTED, connected: false, alert: {} },
        { id: 4, label: "Probe 4", current_temp: DISCONNECTED, connected: false, alert: {} },
      ],
      history: [],
    };
  }

  renderProbeCards();

  // Update battery from most recent history entry
  if (session.history && session.history.length > 0) {
    const last = session.history[session.history.length - 1];
    if (last.battery !== undefined) updateBattery(last.battery);
  }

  // Build chart data from history
  chartData = [[], [], [], [], []];
  if (session.history) {
    session.history.forEach((h) => {
      chartData[0].push(Math.floor(new Date(h.timestamp).getTime() / 1000));
      for (let i = 0; i < 4; i++) {
        const v = (h.temps && h.temps[i] !== undefined && h.temps[i] !== DISCONNECTED) ? h.temps[i] : null;
        chartData[i + 1].push(v);
      }
    });
  }
  initChart();
}

// ── WebSocket ───────────────────────────────────
function connectWS() {
  const proto = location.protocol === "https:" ? "wss:" : "ws:";
  ws = new WebSocket(proto + "//" + location.host + "/api/ws");

  ws.onopen = () => {
    banner.classList.add("hidden");
  };

  ws.onmessage = (evt) => {
    try {
      const msg = JSON.parse(evt.data);
      if (msg.temps) {
        updateProbeFromReading(msg.temps);
        const ts = msg.timestamp ? Math.floor(new Date(msg.timestamp).getTime() / 1000) : Math.floor(Date.now() / 1000);
        appendChartPoint(ts, msg.temps);
      }
      if (msg.battery !== undefined) {
        updateBattery(msg.battery);
      }
    } catch (err) {
      console.error("WS parse error:", err);
    }
  };

  ws.onclose = () => {
    banner.classList.remove("hidden");
    setTimeout(connectWS, 3000);
  };

  ws.onerror = () => {
    ws.close();
  };
}

// ── Commit info ────────────────────────────────
async function loadCommit() {
  try {
    const res = await fetch("/api/version");
    const data = await res.json();
    if (data.commit && data.commit !== "dev" && data.commit !== "unknown") {
      const el = document.getElementById("commit-info");
      const short = data.commit.substring(0, 7);
      const link = document.createElement("a");
      link.href = "https://github.com/stahnma/therm-pro/commit/" + data.commit;
      link.target = "_blank";
      link.rel = "noopener";
      link.textContent = short;
      el.textContent = "build: ";
      el.appendChild(link);
    }
  } catch (e) { /* ignore */ }
}

// ── Auth ────────────────────────────────────────
async function checkAuth() {
  try {
    const resp = await fetch('/api/auth/status');
    const data = await resp.json();
    userRole = data.role;
    canRegister = data.can_register;
    applyRoleUI();
  } catch (err) {
    console.error('Failed to check auth:', err);
  }
}

function applyRoleUI() {
  const resetBtn = document.getElementById('reset-cook');
  if (resetBtn) resetBtn.style.display = userRole === 'admin' ? '' : 'none';

  const signInBtn = document.getElementById('sign-in');
  if (signInBtn) signInBtn.style.display = userRole === 'admin' ? 'none' : '';

  const registerBtn = document.getElementById('register-passkey');
  if (registerBtn) registerBtn.style.display = canRegister ? '' : 'none';
}

async function signIn() {
  try {
    const beginResp = await fetch('/auth/login/begin', { method: 'POST' });
    if (!beginResp.ok) { alert('Sign in not available'); return; }
    const options = await beginResp.json();

    // Convert base64url strings to ArrayBuffers for WebAuthn API
    options.publicKey.challenge = base64urlToBuffer(options.publicKey.challenge);
    if (options.publicKey.allowCredentials) {
      options.publicKey.allowCredentials = options.publicKey.allowCredentials.map(c => ({
        ...c,
        id: base64urlToBuffer(c.id),
      }));
    }

    const credential = await navigator.credentials.get({ publicKey: options.publicKey });

    const finishResp = await fetch('/auth/login/finish', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        id: credential.id,
        rawId: bufferToBase64url(credential.rawId),
        type: credential.type,
        response: {
          authenticatorData: bufferToBase64url(credential.response.authenticatorData),
          clientDataJSON: bufferToBase64url(credential.response.clientDataJSON),
          signature: bufferToBase64url(credential.response.signature),
          userHandle: credential.response.userHandle ? bufferToBase64url(credential.response.userHandle) : null,
        },
      }),
    });

    if (finishResp.ok) {
      location.reload();
    } else {
      alert('Sign in failed');
    }
  } catch (err) {
    console.error('Sign in error:', err);
    alert('Sign in failed: ' + err.message);
  }
}

async function registerPasskey() {
  try {
    const beginResp = await fetch('/auth/register/begin', { method: 'POST' });
    if (!beginResp.ok) { alert('Registration not available'); return; }
    const options = await beginResp.json();

    options.publicKey.challenge = base64urlToBuffer(options.publicKey.challenge);
    options.publicKey.user.id = base64urlToBuffer(options.publicKey.user.id);
    if (options.publicKey.excludeCredentials) {
      options.publicKey.excludeCredentials = options.publicKey.excludeCredentials.map(c => ({
        ...c,
        id: base64urlToBuffer(c.id),
      }));
    }

    const credential = await navigator.credentials.create({ publicKey: options.publicKey });

    const finishResp = await fetch('/auth/register/finish', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        id: credential.id,
        rawId: bufferToBase64url(credential.rawId),
        type: credential.type,
        response: {
          attestationObject: bufferToBase64url(credential.response.attestationObject),
          clientDataJSON: bufferToBase64url(credential.response.clientDataJSON),
        },
      }),
    });

    if (finishResp.ok) {
      alert('Passkey registered!');
    } else {
      alert('Registration failed');
    }
  } catch (err) {
    console.error('Register error:', err);
    alert('Registration failed: ' + err.message);
  }
}

function base64urlToBuffer(base64url) {
  const base64 = base64url.replace(/-/g, '+').replace(/_/g, '/');
  const pad = base64.length % 4;
  const padded = pad ? base64 + '='.repeat(4 - pad) : base64;
  const binary = atob(padded);
  const bytes = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i);
  return bytes.buffer;
}

function bufferToBase64url(buffer) {
  const bytes = new Uint8Array(buffer);
  let binary = '';
  for (let i = 0; i < bytes.length; i++) binary += String.fromCharCode(bytes[i]);
  return btoa(binary).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
}

// ── Boot ────────────────────────────────────────
checkAuth();
loadSession().then(() => {
  connectWS();
});
loadCommit();
