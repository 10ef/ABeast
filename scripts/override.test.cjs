const { readFileSync } = require('node:fs');
const { join } = require('node:path');
const vm = require('node:vm');
const assert = require('node:assert/strict');
const { test } = require('node:test');

const source = readFileSync(join(__dirname, '../clash/exitguard-override.js'), 'utf8');
function apply(config) {
  const context = vm.createContext({ config });
  return vm.runInContext(source + '\nmain(config)', context);
}

test('requires a user secret before creating a controller', () => {
  const config = { rules: ['MATCH,Proxy'] };
  assert.throws(() => apply(config), /private Mihomo secret/);
  assert.equal(config['external-controller'], undefined);
  assert.equal(config['proxy-groups'], undefined);
});

test('retains private secret and creates loopback controller and listeners', () => {
  const config = apply({ secret: 'test-only-secret', rules: ['MATCH,Proxy'] });
  assert.equal(config.secret, 'test-only-secret');
  assert.equal(config['external-controller'], '127.0.0.1:9090');
  assert.equal(config.listeners.length, 2);
  assert.ok(config.listeners.every(listener => listener.listen === '127.0.0.1'));
  assert.ok(config['proxy-groups'].every(group => group.proxies.includes('REJECT')));
});

test('preserves an existing controller and remains idempotent', () => {
  const input = { 'external-controller': '127.0.0.1:9097', secret: 'test-only-secret',
    rules: ['MATCH,Proxy'] };
  const config = apply(apply(input));
  assert.equal(config['external-controller'], '127.0.0.1:9097');
  assert.equal(config.secret, 'test-only-secret');
  assert.equal(config['proxy-groups'].length, 2);
  assert.equal(config.listeners.length, 2);
});
