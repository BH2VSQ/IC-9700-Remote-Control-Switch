// Pure connection-settings helpers so validation and upgrades are testable
// without a radio, Wails runtime, localStorage or UI.
export const DEFAULT_SKYCAT_ADDRESS = '127.0.0.1:4537';

export function isValidSkyCATAddress(value) {
  const match = /^(localhost|127\.0\.0\.1|\[::1\]):([0-9]{1,5})$/i.exec(value?.trim() || '');
  if (!match) return false;
  const port = Number(match[2]);
  return Number.isInteger(port) && port >= 1 && port <= 65535;
}

export function upgradeConnectionConfig(config) {
  if (!config || config.transport !== 'skycat') return config;
  const address = (config.address || '').trim();
  // 4536 was the old hardcoded default and collided with an existing node.exe.
  // Only migrate the exact legacy default, never a custom hostname/port.
  if (!address || /^(localhost|127\.0\.0\.1|\[::1\]):4536$/i.test(address)) {
    return { ...config, address: DEFAULT_SKYCAT_ADDRESS };
  }
  return config;
}
