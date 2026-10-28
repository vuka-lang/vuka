package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestScriptSetup(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "component.mjs"), []byte("const __VUKA_SCRIPT_DEV__ = true;\n"+componentModule), 0o644); err != nil {
		t.Fatal(err)
	}
	src := `import { component, ref, listen, onDestroy } from './component.mjs';
import assert from 'node:assert/strict';
const element = { getAttribute() { return null; } };
const found = {};
const ctx = { ref: Object.assign(() => found, { all: () => () => [found] }), listen: () => 'off', onDestroy: () => {} };
assert.throws(() => ref('dialog'), /ref\(\) called outside a component's setup; call it at the top and keep the element/);
let saved;
const h = component(() => {
 assert.equal(ref('dialog'), found);
 saved = ref.all('item');
 assert.equal(listen(found, 'click', () => {}), 'off');
 onDestroy(() => {});
});
h.mounted(element, ctx);
assert.deepEqual(saved(), [found]);
assert.throws(() => ref('dialog'), /outside a component's setup/);
const broken = component(() => { throw Error('failed'); });
assert.throws(() => broken.mounted(element, ctx), /failed/);
assert.throws(() => ref('dialog'), /outside a component's setup/);
const asyncHook = component(async () => { await Promise.resolve(); assert.throws(() => ref('dialog'), /outside a component's setup/); });
asyncHook.mounted(element, ctx);
await Promise.resolve();
`
	path := filepath.Join(dir, "test.mjs")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(node, path).CombinedOutput(); err != nil {
		t.Fatalf("setup runtime: %v\n%s", err, out)
	}
}
