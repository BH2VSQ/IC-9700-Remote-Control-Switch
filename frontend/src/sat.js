const params = new URLSearchParams(location.search);
const injected = window.__SAT_CONFIG__ || {};
const rpcPort = Number(params.get('port') || injected.port || 0);
const rpcToken = params.get('token') || injected.token || '';
const configuredTheme = (params.get('theme') || injected.theme) === 'dark' ? 'dark' : 'day';
const UI_SCALE_KEY = 'ic9700-remote-io-ui-scale';
const UI_SCALE_OPTIONS = [100, 125];
const RADIO_MODES = ['LSB', 'USB', 'AM', 'CW', 'RTTY', 'FM', 'CW-R', 'RTTY-R', 'DV'];
const $ = (id) => document.getElementById(id);

let refreshInFlight = false;
let lastStateKey = '';
let topmost = false;
const lastSentFrequency = { RX: null, TX: null };
const frequencyInFlight = { RX: false, TX: false };
const pendingFrequency = { RX: null, TX: null };

function applyTheme(theme) {
  document.documentElement.dataset.theme = theme;
}

let activeUIScale = null;

function applyUIScale(value) {
  const savedScale = Number(value);
  const scale = UI_SCALE_OPTIONS.includes(savedScale) ? savedScale : 100;
  if (activeUIScale === scale) return;
  activeUIScale = scale;
  const shell = document.querySelector('.sat-shell');
  if (shell) {
    const inversePercent = 10000 / scale;
    shell.style.width = `${inversePercent}%`;
    shell.style.minHeight = `${inversePercent}%`;
    shell.style.marginInline = '0';
    shell.style.zoom = `${scale}%`;
  }
  // This method is bound by Wails in the SAT helper process and resizes the
  // actual native window frame to match the content scale.
  const resize = window.go?.main?.SatelliteShell?.SetUIScale;
  if (typeof resize === 'function') {
    Promise.resolve().then(() => resize(scale)).catch((err) => console.warn('SAT 窗口缩放失败：', err));
  }
}

async function syncUIScale() {
  try {
    const data = await rpc('/api/sat/ui-scale');
    if (UI_SCALE_OPTIONS.includes(Number(data.scale))) {
      applyUIScale(Number(data.scale));
      return;
    }
  } catch (_) {
    // During startup, use the scale passed to the helper process.
  }
  try {
    applyUIScale(Number(window.__SAT_CONFIG__?.scale || localStorage.getItem(UI_SCALE_KEY)));
  } catch (_) {
    applyUIScale(100);
  }
}

window.addEventListener('storage', (event) => {
  if (event.key === UI_SCALE_KEY) syncUIScale();
});

function setText(id, value) {
  const el = $(id);
  if (el && el.textContent !== value) el.textContent = value;
}

function setClass(id, value) {
  const el = $(id);
  if (el && el.className !== value) el.className = value;
}

function showMessage(text, kind = '') {
  const el = $('satMessage');
  if (!el) return;
  const cls = `message ${kind}`.trim();
  if (el.textContent !== text) el.textContent = text;
  if (el.className !== cls) el.className = cls;
}

function setupModes() {
  for (const id of ['rxModeSelect', 'txModeSelect']) {
    $(id).innerHTML = RADIO_MODES.map((mode) => `<option value="${mode}">${mode}</option>`).join('');
  }
}

function setEnabled(enabled) {
  for (const id of ['rxFreqInput', 'txFreqInput', 'rxModeSelect', 'txModeSelect']) {
    const el = $(id);
    if (el) el.disabled = !enabled;
  }
}

function updateInput(id, value) {
  const el = $(id);
  if (!el || document.activeElement === el) return;
  const next = String(value ?? '');
  if (el.value !== next) el.value = next;
}

function formatFrequency(hz, fallback) {
  const n = Number(hz || 0);
  return n > 0 ? (n / 1e6).toFixed(6) : fallback;
}

async function rpc(path, options = {}) {
  if (!rpcPort || !rpcToken) throw new Error('SAT 窗口通信参数无效');
  const response = await fetch(`http://127.0.0.1:${rpcPort}${path}`, {
    ...options,
    headers: {
      ...(options.headers || {}),
      'X-IC9700-SAT-TOKEN': rpcToken,
      'Content-Type': 'application/json',
    },
    cache: 'no-store',
  });
  let data = {};
  try { data = await response.json(); } catch (_) {}
  if (!response.ok || !data.ok) throw new Error(data.error || `RPC ${response.status}`);
  return data;
}

function updateTopmostUI() {
  const button = $('topmostBtn');
  if (!button) return;
  button.classList.toggle('active', topmost);
  button.setAttribute('aria-pressed', topmost ? 'true' : 'false');
  button.querySelector('.pin-icon').textContent = topmost ? '◆' : '⌖';
  button.querySelector('span:last-child').textContent = topmost ? '已置顶' : '置顶';
}

async function setAlwaysOnTop(enabled) {
  const fn = window.runtime?.WindowSetAlwaysOnTop;
  if (typeof fn !== 'function') {
    throw new Error('当前 Wails Runtime 不支持窗口置顶');
  }
  await fn(enabled);
  topmost = !!enabled;
  updateTopmostUI();
  showMessage(topmost ? 'SAT 窗口已置顶。' : 'SAT 窗口已取消置顶。', 'ok');
}

function focusOwnWindow() {
  try {
    window.runtime?.WindowShow?.();
    window.runtime?.WindowUnminimise?.();
  } catch (_) {}
}

function renderState(data) {
  const connected = !!data.connected;
  const sat = data.satellite || { enabled: false };
  const rxFreq = formatFrequency(sat.rxFrequency, '');
  const txFreq = formatFrequency(sat.txFrequency, '');
  const rxMode = sat.rxMode || 'FM';
  const txMode = sat.txMode || 'FM';
  const key = JSON.stringify({ connected, sat: !!sat.enabled, rxFreq, txFreq, rxMode, txMode });

  if (key !== lastStateKey) {
    lastStateKey = key;
    setClass('satState', `sat-state-pill ${sat.enabled ? 'sat-on' : 'sat-off'}`);
    setText('satState', sat.enabled ? 'ON' : 'OFF');
    setText('satModeText', !connected ? '未连接' : sat.enabled ? '卫星模式已启用' : '未启用');
    $('satToggleBtn').disabled = !connected;
    $('satToggleBtn').textContent = sat.enabled ? '关闭' : '开启';
    setEnabled(connected && sat.enabled);

    if (sat.enabled) {
      updateInput('rxFreqInput', rxFreq);
      updateInput('txFreqInput', txFreq);
      if (document.activeElement !== $('rxModeSelect')) $('rxModeSelect').value = rxMode;
      if (document.activeElement !== $('txModeSelect')) $('txModeSelect').value = txMode;
      if (Number.isFinite(Number(sat.rxFrequency))) lastSentFrequency.RX = Number(sat.rxFrequency);
      if (Number.isFinite(Number(sat.txFrequency))) lastSentFrequency.TX = Number(sat.txFrequency);
    }
  }

  if (!connected) showMessage('连接电台后读取卫星状态。');
  else if (!sat.enabled) showMessage('卫星模式未启用。');
}

async function refresh() {
  if (refreshInFlight) return;
  refreshInFlight = true;
  try {
    const data = await rpc('/api/sat/state', { method: 'GET', headers: {} });
    renderState(data);
  } catch (err) {
    setEnabled(false);
    $('satToggleBtn').disabled = true;
    showMessage(String(err?.message || err), 'error');
  } finally {
    refreshInFlight = false;
  }
}

async function toggleSatellite() {
  try {
    $('satToggleBtn').disabled = true;
    const data = await rpc('/api/sat/state', { method: 'GET', headers: {} });
    await rpc('/api/sat/mode', {
      method: 'POST',
      body: JSON.stringify({ enabled: !data.satellite?.enabled })
    });
    await refresh();
    showMessage(data.satellite?.enabled ? '卫星模式已退出。' : '卫星模式已启用。', 'ok');
  } catch (err) {
    showMessage(String(err?.message || err), 'error');
    await refresh();
  }
}

function parseFrequencyInput(id) {
  const value = Number($(id).value);
  if (!Number.isFinite(value) || value <= 0) throw new Error('频率无效');
  return Math.round(value * 1e6);
}

async function commitFrequency(side) {
  const id = side === 'RX' ? 'rxFreqInput' : 'txFreqInput';
  let hz;
  try {
    hz = parseFrequencyInput(id);
  } catch (err) {
    showMessage(String(err?.message || err), 'error');
    return;
  }
  if (lastSentFrequency[side] === hz && !frequencyInFlight[side]) return;
  pendingFrequency[side] = hz;
  if (frequencyInFlight[side]) return;

  frequencyInFlight[side] = true;
  try {
    while (pendingFrequency[side] !== null) {
      const nextHz = pendingFrequency[side];
      pendingFrequency[side] = null;
      await rpc('/api/sat/frequency', {
        method: 'POST',
        body: JSON.stringify({ side, hz: nextHz })
      });
      lastSentFrequency[side] = nextHz;
    }
    showMessage(`${side} 频率已更新。`, 'ok');
  } catch (err) {
    showMessage(`${side} 频率切换失败：${err?.message || err}`, 'error');
    await refresh();
  } finally {
    frequencyInFlight[side] = false;
  }
}

async function changeMode(side) {
  const id = side === 'RX' ? 'rxModeSelect' : 'txModeSelect';
  try {
    await rpc('/api/sat/mode-type', {
      method: 'POST',
      body: JSON.stringify({ side, mode: $(id).value })
    });
    showMessage(`${side} 模式已切换为 ${$(id).value}。`, 'ok');
  } catch (err) {
    showMessage(`${side} 模式切换失败：${err?.message || err}`, 'error');
    await refresh();
  }
}

$('satToggleBtn').onclick = toggleSatellite;
$('topmostBtn').onclick = async () => {
  try { await setAlwaysOnTop(!topmost); }
  catch (err) { showMessage(String(err?.message || err), 'error'); }
};

for (const [side, id] of [['RX', 'rxFreqInput'], ['TX', 'txFreqInput']]) {
  $(id).addEventListener('keydown', (event) => {
    if (event.key === 'Enter') {
      event.preventDefault();
      commitFrequency(side);
      $(id).blur();
    }
  });
  $(id).addEventListener('blur', () => commitFrequency(side));
}

$('rxModeSelect').addEventListener('change', () => changeMode('RX'));
$('txModeSelect').addEventListener('change', () => changeMode('TX'));

applyTheme(configuredTheme);
syncUIScale();
setupModes();
updateTopmostUI();
setEnabled(false);
focusOwnWindow();
refresh();
window.setInterval(refresh, 3500);
window.setInterval(syncUIScale, 1000);
