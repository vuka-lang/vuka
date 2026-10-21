import assert from 'node:assert/strict';
import toolchain from '../toolchain.js';

for (const version of ['v0.50.0', 'v1.0.0', 'v1.3.0', 'v1.3.99']) {
  assert.equal(toolchain.supportsTemplateContext(`vuka ${version} (vuka, ui, web v1.4.0)`), false, version);
}
for (const version of ['v1.4.0', 'v1.4.1', 'v1.10.0', 'v2.0.0']) {
  assert.equal(toolchain.supportsTemplateContext(`vuka ${version} (vuka, ui, web v1.3.0)`), true, version);
}
assert.equal(toolchain.supportsTemplateContext('vuka dev'), undefined);
assert.equal(toolchain.supportsTemplateContext('error: v1.4.0'), undefined);
assert.equal(toolchain.serverVersion('vuka v1.4.0 (vuka, ui, web v1.4.0)').text, 'v1.4.0');
assert.equal(toolchain.updateCommand, 'go install github.com/vuka-lang/vuka/cmd/vuka@v1.4.0');
console.log('toolchain tests passed');
