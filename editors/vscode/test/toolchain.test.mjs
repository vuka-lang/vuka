import assert from 'node:assert/strict';
import toolchain from '../toolchain.js';

for (const version of ['v0.50.0', 'v1.0.0', 'v1.4.0', 'v1.99.0']) {
  assert.equal(toolchain.supportsShapes(`vuka ${version} (vuka, ui, web v1.4.0)`), false, version);
}
for (const version of ['v2.0.0', 'v2.0.1', 'v2.10.0', 'v3.0.0']) {
  assert.equal(toolchain.supportsShapes(`vuka ${version} (vuka, ui, web v1.3.0)`), true, version);
}
assert.equal(toolchain.supportsShapes('vuka dev'), undefined);
assert.equal(toolchain.supportsShapes('error: v1.4.0'), undefined);
assert.equal(toolchain.serverVersion('vuka v1.4.0 (vuka, ui, web v1.4.0)').text, 'v1.4.0');
assert.equal(toolchain.updateCommand, 'go install github.com/vuka-lang/vuka/cmd/vuka/v2@latest');
console.log('toolchain tests passed');
