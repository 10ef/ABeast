const { readFileSync } = require('node:fs');
const { join } = require('node:path');
const vm = require('node:vm');
const assert = require('node:assert/strict');
const { test } = require('node:test');

const source = readFileSync(join(__dirname, '../clash/exitguard-override.js'), 'utf8');
function apply(config, upstreams = {}) {
  const context = vm.createContext({ config, upstreams });
  return vm.runInContext(source + '\nObject.assign(UPSTREAMS, upstreams); main(config)', context);
}

function input(rules) {
  return { secret: 'test-only-secret', rules, 'proxy-groups': [{ name: 'Proxy', type: 'select' }] };
}

test('requires a user secret before creating a controller', () => {
  const config = { rules: ['MATCH,Proxy'] };
  assert.throws(() => apply(config), /private Mihomo secret/);
  assert.equal(config['external-controller'], undefined);
  assert.equal(config['proxy-groups'], undefined);
});

test('retains private secret and creates loopback controller and listeners', () => {
  const config = apply(input(['MATCH,Proxy']));
  assert.equal(config.secret, 'test-only-secret');
  assert.equal(config['external-controller'], '127.0.0.1:9090');
  assert.equal(config.listeners.length, 2);
  assert.ok(config.listeners.every(listener => listener.listen === '127.0.0.1'));
  assert.ok(config['proxy-groups'].slice(1).every(group => group.proxies.includes('REJECT')));
  assert.ok(config.listeners.every(listener => listener.proxy === 'Proxy'));
  assert.ok(!config.rules.some(rule => /cursor|copilot/i.test(rule)));
});

test('preserves an existing controller and remains idempotent', () => {
  const config = apply(apply({ ...input(['MATCH,Proxy']), 'external-controller': '127.0.0.1:9097' }));
  assert.equal(config['external-controller'], '127.0.0.1:9097');
  assert.equal(config.secret, 'test-only-secret');
  assert.equal(config['proxy-groups'].length, 3);
  assert.equal(config.listeners.length, 2);
});

test('requires an explicit upstream when a prior rule makes the policy ambiguous', () => {
  for (const rule of ['RULE-SET,ai,DIRECT', 'GEOSITE,openai,DIRECT', 'DOMAIN,api.openai.com,DIRECT']) {
    const config = input([rule, 'MATCH,Proxy']);
    assert.throws(() => apply(config), /UPSTREAMS\.openai/);
    assert.deepEqual(config.rules, [rule, 'MATCH,Proxy']);
    assert.equal(config.listeners, undefined);
    assert.equal(apply(config, { openai: 'Proxy', anthropic: 'Proxy' }).listeners[0].proxy, 'Proxy');
  }
});

test('does not pick an arbitrary group, an undefined upstream, or DIRECT', () => {
  for (const rules of [[], ['MATCH,Missing'], ['MATCH,DIRECT']]) {
    assert.throws(() => apply(input(rules)), /UPSTREAMS\.openai/);
  }
  assert.throws(() => apply(input(['MATCH,Proxy']), { openai: 'DIRECT' }), /UPSTREAMS\.openai/);
});

test('honors a consistent domain policy ahead of a direct MATCH', () => {
  const config = apply(input(['DOMAIN-SUFFIX,com,Proxy', 'DOMAIN-SUFFIX,ai,Proxy', 'MATCH,DIRECT']));
  assert.ok(config.listeners.every(listener => listener.proxy === 'Proxy'));
});

test('adds the Claude content suffix and checks only its domain subtree', () => {
  const config = apply(input(['MATCH,Proxy']));
  assert.ok(config.rules.includes('DOMAIN-SUFFIX,claudeusercontent.com,🛡 EG-Anthropic'));

  for (const host of ['claudeusercontent.com', 'preview.frame.claudeusercontent.com']) {
    assert.throws(() => apply(input([`DOMAIN,${host},DIRECT`, 'MATCH,Proxy'])), /UPSTREAMS\.anthropic/);
  }
  for (const host of ['evilclaudeusercontent.com', 'claudeusercontent.com.example.org']) {
    assert.doesNotThrow(() => apply(input([`DOMAIN,${host},DIRECT`, 'MATCH,Proxy'])));
  }
});

test('adds only opted-in service rules without extra probe listeners', () => {
  const settings = { cursor: 'Proxy', copilot: 'Proxy' };
  const config = apply(apply(input(['MATCH,Proxy']), settings), settings);
  assert.equal(config.rules.filter(rule => rule.includes('EG-Cursor')).length, 2);
  assert.equal(config.rules.filter(rule => rule.includes('EG-Copilot')).length, 2);
  assert.ok(!config.rules.some(rule => rule.startsWith('DOMAIN-SUFFIX,github.com,')));
  assert.equal(config.listeners.length, 2);
  assert.ok(!apply(config).rules.some(rule => /EG-Cursor|EG-Copilot/.test(rule)));
});
