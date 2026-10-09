import {
  ListSerialPorts,
  Connect,
  ConnectSkyCAT,
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
  CheckForUpdates,
  DownloadAndInstall,
  Exit,
  OpenSatelliteWindow,
} from './wailsjs/go/main/App.js';

import {
  DEFAULT_SKYCAT_ADDRESS,
  isValidSkyCATAddress,
  upgradeConnectionConfig,
} from './connection-settings.mjs';

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
    : saved?.transport === 'skycat'
      ? `SkyCAT ${saved.address || DEFAULT_SKYCAT_ADDRESS} · 已保存`
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
  let saved;
  try {
    saved = JSON.parse(localStorage.getItem(STORAGE_KEY) || 'null');
  } catch {
    return null;
  }
  const upgraded = upgradeConnectionConfig(saved);
  if (upgraded !== saved) {
    try { localStorage.setItem(STORAGE_KEY, JSON.stringify(upgraded)); } catch {}
  }
  return upgraded;
}

function saveConnection(port, baud, transport = 'serial', address = DEFAULT_SKYCAT_ADDRESS) {
  const config = { port, baud, transport, address };
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

function updateTransportFields() {
  const skycat = $('transportSelect').value === 'skycat';
  $('serialConnectionFields').style.display = skycat ? 'none' : 'grid';
  $('skycatConnectionFields').style.display = skycat ? 'grid' : 'none';
}

function openConnectDialog(message = '') {
  const saved = getSavedConnection();
  $('connectOverlay').classList.remove('hidden');
  $('transportSelect').value = saved?.transport === 'skycat' ? 'skycat' : 'serial';
  $('skycatAddress').value = saved?.address || DEFAULT_SKYCAT_ADDRESS;
  $('baudSelect').value = String(saved?.baud || 115200);
  updateTransportFields();
  $('connectDialogMessage').textContent = message || '选择端口和波特率后点击“保存”。保存后请手动点击主界面的“连接”。';
  // RS-BA1 virtual COM enumeration can take time; do not enumerate serial
  // ports merely because the SkyCAT TCP dialog was opened.
  if ($('transportSelect').value === 'serial') {
    refreshPorts(saved?.port).catch((err) => {
      if ($('transportSelect').value === 'serial')
        $('connectDialogMessage').textContent = `串口扫描失败：${err?.message || err}`;
    });
  }
}

function closeConnectDialog() {
  $('connectOverlay').classList.add('hidden');
}

async function attemptConnectFromSaved() {
  const saved = getSavedConnection();
  if (!saved || (saved.transport !== 'skycat' && !saved.port)) {
    openConnectDialog('请先配置串口或 SkyCAT TCP 地址。');
    return false;
  }
  try {
    const isSkyCAT = saved.transport === 'skycat';
    showMessage(`正在连接 ${isSkyCAT ? saved.address : saved.port}…`);
    if (isSkyCAT) await ConnectSkyCAT(saved.address || DEFAULT_SKYCAT_ADDRESS);
    else await Connect(saved.port, Number(saved.baud || 115200));
    // Connection success should not wait on serial CI-V queries.
    setConnectionUI(true, isSkyCAT ? `SkyCAT ${saved.address || DEFAULT_SKYCAT_ADDRESS}` : saved.port, saved);
    showMessage('连接成功，正在读取电台设置…', 'ok');
    void refreshAll().then(() => {
      showMessage('电台状态读取完成', 'ok');
    }).catch(err => {
      showMessage(`读取电台状态失败：${err?.message || err}`, 'error');
    });
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
    $('satMenuBtn').disabled = status.transport === 'skycat';
    $('satMenuBtn').title = status.transport === 'skycat'
      ? 'SkyCAT 辅助端口不允许修改卫星 VFO 或模式，请使用 SkyRoof'
      : '卫星控制';
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
$('transportSelect').onchange = () => {
  updateTransportFields();
  if ($('transportSelect').value === 'serial') {
    refreshPorts($('portSelect').value).catch((err) => {
      if ($('transportSelect').value === 'serial')
        $('connectDialogMessage').textContent = `串口扫描失败：${err?.message || err}`;
    });
  }
};
$('refreshPorts').onclick = () => refreshPorts($('portSelect').value).catch((err) => {
  $('connectDialogMessage').textContent = `串口扫描失败：${err?.message || err}`;
});
$('applyConnectBtn').onclick = async () => {
  const port = $('portSelect').value;
  const baud = Number($('baudSelect').value);
  const transport = $('transportSelect').value;
  const address = $('skycatAddress').value.trim() || DEFAULT_SKYCAT_ADDRESS;
  if (transport === 'serial' && !port) {
    $('connectDialogMessage').textContent = '请先选择有效串口。';
    return;
  }
  if (transport === 'skycat' && !isValidSkyCATAddress(address)) {
    $('connectDialogMessage').textContent = '请输入有效的本机 SkyCAT 地址，如 127.0.0.1:4537。';
    return;
  }
  try {
    $('applyConnectBtn').disabled = true;
    $('connectDialogMessage').textContent = '正在保存连接设置…';
    const saved = saveConnection(port, baud, transport, address);
    closeConnectDialog();
    // Saving config is a local operation; never await radio readback here.
    showMessage(transport === 'skycat'
      ? `已保存 SkyCAT 独立端口 ${address}，请手动点击“连接”`
      : `端口配置已保存：${port} · ${baud} bps，请手动点击“连接”`, 'ok');
    if (!$('connectionLamp').classList.contains('online'))
      setConnectionUI(false, '', saved);
  } catch (err) {
    $('connectDialogMessage').textContent = `无法保存设置：${err?.message || err}`;
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

$('updateBtn').onclick = async () => {
  const btn = $('updateBtn');
  if (btn.disabled) return;
  btn.disabled = true;
  const original = btn.textContent;
  try {
    btn.textContent = '检查中…';
    const info = await CheckForUpdates();
    if (!info.hasUpdate) {
      showMessage(`已是最新版本 v${info.currentVersion}`, 'ok');
      return;
    }
    btn.textContent = '更新中…';
    showMessage(`发现新版本 v${info.latestVersion}，正在下载更新…`);
    await DownloadAndInstall();
    showMessage('更新已下载，正在重启应用…', 'ok');
  } catch (err) {
    console.error(err);
    showMessage(`检查更新失败：${err?.message || err}`, 'error');
  } finally {
    btn.disabled = false;
    btn.textContent = original;
  }
};

makeChoiceButtons('dataOffButtons', SetDataOffInput);
makeChoiceButtons('dataButtons', SetDataInput);
applyTheme(localStorage.getItem(THEME_KEY) === 'dark' ? 'dark' : 'day');

const savedOnStartup = getSavedConnection();
if (savedOnStartup?.transport !== 'skycat')
  void refreshPorts(savedOnStartup?.port).catch(() => {});
void refresh().catch(() => {});
// Connection is always a manual action. Auxiliary settings are read immediately
// after a successful connection or explicit full-state refresh, not by auto-connect.
setInterval(() => refresh().catch(() => {}), 5000);
