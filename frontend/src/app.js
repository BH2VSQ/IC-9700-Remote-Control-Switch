import {
  ListSerialPorts,
  Connect,
  Disconnect,
  RefreshStatus,
  RefreshRadioAssist,
  SetDataOffInput,
  SetDataInput,
  SetAllLAN,
  SetAllUSB,
  SetUSBOutput,
  SetSpeechCompressor,
  SetCompLevel,
  SetCWKeyingSpeed,
  SetRFPower,
  Exit,
  OpenSatelliteWindow,
} from './wailsjs/go/main/App.js';

const INPUTS = ['MIC', 'ACC', 'MIC + ACC', 'USB', 'MIC + USB', 'LAN'];
const STORAGE_KEY = 'ic9700-remote-io-connection';
const THEME_KEY = 'ic9700-remote-io-theme';
let refreshingMain = false;
let refreshingAssist = false;
let assistState = null;
let lastSatelliteMode = null;

const $ = (id) => document.getElementById(id);

function showMessage(text, kind = '') {
  const el = $('message');
  el.textContent = text;
  el.className = `message ${kind}`;
}

function showSatMessage(text, kind = '') {
  const el = $('satMessage');
  el.textContent = text;
  el.className = `message ${kind}`;
}

function setConnectionUI(connected, port = '', saved = null) {
  $('connectionLamp').className = `lamp ${connected ? 'online' : 'offline'}`;
  $('connectionText').textContent = connected ? '已连接' : '未连接';
  $('connectionDetail').textContent = connected
    ? `${port} · CI-V`
    : saved?.port ? `${saved.port} · ${saved.baud} bps · 已保存` : '未配置端口';
  $('mainConnectBtn').textContent = connected ? '断开连接' : '连接';
}

function makeChoiceButtons(containerId, setter) {
  const host = $(containerId);
  host.innerHTML = '';
  for (const value of INPUTS) {
    const button = document.createElement('button');
    button.className = 'choice-btn';
    button.textContent = value;
    button.onclick = async () => {
      await perform(async () => {
        await requireConnected();
        await setter(value);
        await refresh();
      }, `设置 ${value}`);
    };
    host.appendChild(button);
  }
}

function setSelected(containerId, value) {
  for (const b of $(containerId).querySelectorAll('.choice-btn')) {
    b.classList.toggle('selected', b.textContent === value);
  }
}

function setStatePill(id, value, extra = '') {
  const el = $(id);
  el.textContent = value || '—';
  el.className = `state-pill ${value ? 'known' : 'unknown'} ${extra}`.trim();
}

async function perform(action, label) {
  try {
    showMessage(`${label}…`);
    await action();
    showMessage(`${label}完成`, 'ok');
  } catch (err) {
    console.error(err);
    showMessage(String(err?.message || err), 'error');
  }
}

function getSavedConnection() {
  try {
    return JSON.parse(localStorage.getItem(STORAGE_KEY) || 'null');
  } catch {
    return null;
  }
}

function saveConnection(port, baud) {
  const config = { port, baud };
  localStorage.setItem(STORAGE_KEY, JSON.stringify(config));
  return config;
}

async function refreshPorts(selected = '') {
  const ports = await ListSerialPorts();
  const select = $('portSelect');
  select.innerHTML = '';
  if (!ports.length) {
    const option = document.createElement('option');
    option.value = '';
    option.textContent = '未发现串口';
    select.appendChild(option);
    return;
  }
  for (const port of ports) {
    const option = document.createElement('option');
    option.value = port;
    option.textContent = port;
    select.appendChild(option);
  }
  const preferred = selected || getSavedConnection()?.port;
  if (preferred && ports.includes(preferred)) select.value = preferred;
}

function openConnectDialog(message = '') {
  const saved = getSavedConnection();
  $('connectOverlay').classList.remove('hidden');
  $('baudSelect').value = String(saved?.baud || 115200);
  $('connectDialogMessage').textContent = message || '选择端口和波特率后点击“保存”。保存后请手动点击主界面的“连接”。';
  refreshPorts(saved?.port).catch((err) => {
    $('connectDialogMessage').textContent = `串口扫描失败：${err?.message || err}`;
  });
}

function closeConnectDialog() {
  $('connectOverlay').classList.add('hidden');
}

async function attemptConnectFromSaved() {
  const saved = getSavedConnection();
  if (!saved?.port) {
    openConnectDialog('尚未配置串口，请先保存端口配置。');
    return false;
  }
  try {
    showMessage(`正在连接 ${saved.port}…`);
    await Connect(saved.port, Number(saved.baud || 115200));
    await refreshAll();
    showMessage('连接成功，已读取电台当前设置', 'ok');
    return true;
  } catch (err) {
    showMessage(`连接失败：${err?.message || err}`, 'error');
    return false;
  }
}

async function requireConnected() {
  const status = await RefreshStatus();
  if (status.connected) return status;
  throw new Error('请先点击“连接”按钮连接电台');
}

function applyAssistEnabled(connected) {
  for (const element of document.querySelectorAll('.assist-control')) {
    element.querySelectorAll('button, input').forEach((control) => {
      control.disabled = !connected;
    });
  }
}

function setAssistState(data) {
  assistState = data || null;
  const connected = !!data?.connected;
  const knownConnected = !!data?.speechCompressorKnown;
  const stateEl = $('compressorState');
  stateEl.textContent = knownConnected ? (data.speechCompressor ? 'ON' : 'OFF') : '—';
  stateEl.className = `assist-state ${knownConnected ? (data.speechCompressor ? 'on' : 'off') : 'unknown'}`;

  const overallKnown = knownConnected ||
    (Number.isInteger(data?.compLevel) && data.compLevel >= 0 && data.compLevel <= 10) ||
    (Number.isInteger(data?.cwKeyingSpeed) && data.cwKeyingSpeed >= 6 && data.cwKeyingSpeed <= 48) ||
    (Number.isInteger(data?.rfPower) && data.rfPower >= 0 && data.rfPower <= 100);
  $('assistStatePill').textContent = overallKnown ? '已同步' : '—';
  $('assistStatePill').className = `state-pill ${overallKnown ? 'known' : 'unknown'}`;

  $('compressorToggleBtn').textContent = knownConnected
    ? (data.speechCompressor ? '关闭压缩' : '开启压缩')
    : '压缩开关';

  $('compLevelValue').textContent = Number.isInteger(data?.compLevel) && data.compLevel >= 0
    ? `LEVEL ${data.compLevel}`
    : 'LEVEL —';
  $('compLevelValue').classList.toggle('known', Number.isInteger(data?.compLevel) && data.compLevel >= 0);

  if (Number.isInteger(data?.cwKeyingSpeed) && data.cwKeyingSpeed >= 6 && data.cwKeyingSpeed <= 48) {
    $('cwSpeedValue').textContent = `${data.cwKeyingSpeed} WPM`;
    $('cwSpeedSlider').value = String(data.cwKeyingSpeed);
  } else {
    $('cwSpeedValue').textContent = '— WPM';
  }

  if (Number.isInteger(data?.rfPower) && data.rfPower >= 0 && data.rfPower <= 100) {
    $('rfPowerValue').textContent = `${data.rfPower}%`;
    $('rfPowerSlider').value = String(data.rfPower);
  } else {
    $('rfPowerValue').textContent = '—%';
  }

  applyAssistEnabled(connected);
}

async function refresh() {
  if (refreshingMain) return null;
  refreshingMain = true;
  try {
    const status = await RefreshStatus();
    const saved = getSavedConnection();
    setConnectionUI(status.connected, status.port, saved);
    setStatePill('dataOffState', status.dataOffInput);
    setStatePill('dataState', status.dataInput);
    setStatePill('usbState', status.usbOutput);
    setSelected('dataOffButtons', status.dataOffInput);
    setSelected('dataButtons', status.dataInput);
    $('lastTX').textContent = status.lastTX || 'No data';
    $('lastRX').textContent = status.lastRX || 'No data';

    // Entering/exiting SAT changes the active CI-V context. The RF power
    // control uses command 14/0A against that current context, so stale UI
    // values are unsafe. Detect the SAT transition in the normal lightweight
    // status poll and immediately re-read the full radio-assist state.
    const satelliteModeChanged = status.connected &&
      status.satelliteModeKnown &&
      lastSatelliteMode !== null &&
      status.satelliteMode !== lastSatelliteMode;

    if (status.connected && status.satelliteModeKnown) {
      lastSatelliteMode = !!status.satelliteMode;
    } else if (!status.connected) {
      lastSatelliteMode = null;
    }

    for (const button of document.querySelectorAll('.choice-btn, .output-btn, .wide-action')) {
      button.disabled = !status.connected;
    }
    $('refreshState').disabled = !status.connected;
    $('satMenuBtn').disabled = false;
    if (!status.connected) {
      setAssistState(null);
    }
    if (status.error && status.connected) showMessage(status.error, 'error');
    if (satelliteModeChanged) {
      await refreshRadioAssist();
    }
    return status;
  } finally {
    refreshingMain = false;
  }
}

async function refreshRadioAssist() {
  if (refreshingAssist) return null;
  refreshingAssist = true;
  try {
    const data = await RefreshRadioAssist();
    setAssistState(data);
    if (data.lastTX) $('lastTX').textContent = data.lastTX;
    if (data.lastRX) $('lastRX').textContent = data.lastRX;
    if (data.error && data.speechCompressorKnown) showMessage(data.error, 'error');
    return data;
  } finally {
    refreshingAssist = false;
  }
}

async function refreshAll() {
  const status = await refresh();
  if (status?.connected) await refreshRadioAssist();
  else setAssistState(null);
  return status;
}

async function writeAssist(action, label) {
  await perform(async () => {
    await requireConnected();
    await action();
    await refreshRadioAssist();
  }, label);
}

function setupWheelSlider(wrapperId, sliderId, min, max, step, valueWriter) {
  const wrapper = $(wrapperId);
  const slider = $(sliderId);
  wrapper.addEventListener('wheel', (event) => {
    if (slider.disabled) return;
    event.preventDefault();
    const direction = event.deltaY < 0 ? 1 : -1;
    const current = Number(slider.value);
    const next = Math.max(min, Math.min(max, current + direction * step));
    if (next === current) return;
    slider.value = String(next);
    valueWriter(next);
  }, { passive: false });
}

function applyTheme(theme) {
  document.documentElement.dataset.theme = theme;
  $('themeBtn').textContent = theme === 'day' ? '☀' : '☾';
  localStorage.setItem(THEME_KEY, theme);
}

$('exitBtn').onclick = async () => {
  try { await Exit(); } catch (err) { console.error(err); }
};

$('connectMenuBtn').onclick = () => openConnectDialog();
$('closeConnectBtn').onclick = closeConnectDialog;
$('cancelConnectBtn').onclick = closeConnectDialog;
$('connectOverlay').addEventListener('click', (event) => {
  if (event.target === $('connectOverlay')) closeConnectDialog();
});
$('refreshPorts').onclick = () => refreshPorts($('portSelect').value).catch((err) => {
  $('connectDialogMessage').textContent = `串口扫描失败：${err?.message || err}`;
});
$('applyConnectBtn').onclick = async () => {
  const port = $('portSelect').value;
  const baud = Number($('baudSelect').value);
  if (!port) {
    $('connectDialogMessage').textContent = '请先选择有效串口。';
    return;
  }
  try {
    $('applyConnectBtn').disabled = true;
    $('connectDialogMessage').textContent = `正在保存 ${port}…`;
    saveConnection(port, baud);
    closeConnectDialog();
    await refresh();
    showMessage(`端口配置已保存：${port} · ${baud} bps，请手动点击“连接”`, 'ok');
  } finally {
    $('applyConnectBtn').disabled = false;
  }
};

$('mainConnectBtn').onclick = async () => {
  const status = await RefreshStatus();
  if (status.connected) {
    await perform(async () => {
      await Disconnect();
      assistState = null;
      await refresh();
      setAssistState(null);
    }, '断开连接');
    return;
  }
  await attemptConnectFromSaved();
};

$('refreshState').onclick = () => perform(refreshAll, '读取状态');
$('setLanBtn').onclick = () => perform(async () => {
  await requireConnected();
  await SetAllLAN();
  await refreshAll();
}, '设置 LAN');
$('setUsbBtn').onclick = () => perform(async () => {
  await requireConnected();
  await SetAllUSB();
  await refreshAll();
}, '设置 USB');
$('usbAfBtn').onclick = () => perform(async () => {
  await requireConnected();
  await SetUSBOutput('AF');
  await refresh();
}, '切换 AF');
$('usbIfBtn').onclick = () => perform(async () => {
  await requireConnected();
  await SetUSBOutput('IF');
  await refresh();
}, '切换 IF');

$('compressorToggleBtn').onclick = async () => {
  if (!assistState?.speechCompressorKnown) {
    showMessage('请先连接电台并读取辅助设置', 'error');
    return;
  }
  await writeAssist(
    () => SetSpeechCompressor(!assistState.speechCompressor),
    assistState.speechCompressor ? '关闭话音压缩' : '开启话音压缩',
  );
};

$('compLevelMinusBtn').onclick = async () => {
  const level = Number(assistState?.compLevel);
  if (!Number.isInteger(level)) return;
  await writeAssist(() => SetCompLevel(Math.max(0, level - 1)), `COMP LEVEL ${Math.max(0, level - 1)}`);
};

$('compLevelPlusBtn').onclick = async () => {
  const level = Number(assistState?.compLevel);
  if (!Number.isInteger(level)) return;
  await writeAssist(() => SetCompLevel(Math.min(10, level + 1)), `COMP LEVEL ${Math.min(10, level + 1)}`);
};

$('cwSpeedSlider').addEventListener('change', async (event) => {
  const value = Number(event.target.value);
  await writeAssist(() => SetCWKeyingSpeed(value), `CW ${value} WPM`);
});

$('rfPowerSlider').addEventListener('change', async (event) => {
  const value = Number(event.target.value);
  await writeAssist(() => SetRFPower(value), `RF POWER ${value}%`);
});

setupWheelSlider('cwSpeedWheelZone', 'cwSpeedSlider', 6, 48, 1, (value) => {
  writeAssist(() => SetCWKeyingSpeed(value), `CW ${value} WPM`);
});

setupWheelSlider('rfPowerWheelZone', 'rfPowerSlider', 0, 100, 1, (value) => {
  writeAssist(() => SetRFPower(value), `RF POWER ${value}%`);
});

$('satMenuBtn').onclick = async () => {
  try {
    await OpenSatelliteWindow(document.documentElement.dataset.theme || 'day');
  } catch (err) {
    showMessage(`打开 SAT 窗口失败：${err?.message || err}`, 'error');
  }
};

$('themeBtn').onclick = () => {
  applyTheme(document.documentElement.dataset.theme === 'day' ? 'dark' : 'day');
};

makeChoiceButtons('dataOffButtons', SetDataOffInput);
makeChoiceButtons('dataButtons', SetDataInput);
applyTheme(localStorage.getItem(THEME_KEY) === 'dark' ? 'dark' : 'day');

await refreshPorts(getSavedConnection()?.port).catch(() => {});
await refresh();
// Connection is always a manual action. Auxiliary settings are read immediately
// after a successful connection or explicit full-state refresh, not by auto-connect.
setInterval(() => refresh().catch(() => {}), 5000);
