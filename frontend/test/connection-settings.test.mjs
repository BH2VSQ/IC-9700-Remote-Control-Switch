import test from 'node:test';
import assert from 'node:assert/strict';
import {
  DEFAULT_SKYCAT_ADDRESS,
  isValidSkyCATAddress,
  upgradeConnectionConfig,
} from '../src/connection-settings.mjs';

test('accepts real loopback addresses and port 4537', () => {
  assert.equal(DEFAULT_SKYCAT_ADDRESS, '127.0.0.1:4537');
  for (const value of ['127.0.0.1:4537', 'localhost:4537', '[::1]:4537', '127.0.0.1:4536']) {
    assert.equal(isValidSkyCATAddress(value), true, value);
  }
});

test('rejects non-loopback, missing or invalid ports', () => {
  for (const value of ['127x0x0x1:4537', '127\\0\\0\\1:4537', '192.168.1.1:4537',
    '127.0.0.2:4537', '127.0.0.1:0', 'localhost:65536',
    'localhost:-1', 'localhost', '127.0.0.1:4537/path']) {
    assert.equal(isValidSkyCATAddress(value), false, value);
  }
});

test('migrates only old SkyCAT default 4536 without overwriting custom port', () => {
  for (const oldAddr of ['127.0.0.1:4536', 'localhost:4536', '[::1]:4536', '']) {
    const old = { transport: 'skycat', address: oldAddr, port: '', baud: 115200 };
    const updated = upgradeConnectionConfig(old);
    assert.deepEqual(updated, { ...old, address: '127.0.0.1:4537' });
    assert.equal(old.address, oldAddr);
  }
  const custom = { transport: 'skycat', address: 'localhost:9999' };
  assert.equal(upgradeConnectionConfig(custom), custom);
  const serial = { transport: 'serial', port: 'COM9', address: '127.0.0.1:4536' };
  assert.equal(upgradeConnectionConfig(serial), serial);
});
