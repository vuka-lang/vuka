// The playground's compiler: vuka.wasm, run in this worker so the page never
// waits on it. Messages are {id, op: 'transpile', files} and
// {id, op: 'format', path, src}; each is answered {id, ok, out | error}.
importScripts('wasm_exec.js')

const ready = (async () => {
  const go = new Go()
  const wasm = fetch('vuka.wasm', { cache: 'no-cache' })
  const types = fetch('types.bin', { cache: 'no-cache' })
  const failed = (r) => {
    if (r.status === 404) throw new Error(`${r.url} isn't built: run npm run playground in docs/`)
    if (!r.ok) throw new Error(`${r.url}: ${r.status} ${r.statusText}`)
    return r
  }
  let instance
  try {
    ;({ instance } = await WebAssembly.instantiateStreaming(wasm.then(failed), go.importObject))
  } catch (err) {
    if (err instanceof TypeError && /MIME|Content-Type/i.test(err.message)) {
      ;({ instance } = await WebAssembly.instantiate(await (await wasm).arrayBuffer(), go.importObject))
    } else {
      throw err
    }
  }
  go.run(instance)
  const err = globalThis.vukaLoad(new Uint8Array(await (await types.then(failed)).arrayBuffer()))
  if (err) throw new Error(err)
})()

onmessage = async ({ data }) => {
  const { id, op } = data
  try {
    await ready
    let out
    if (op === 'transpile') {
      const start = performance.now()
      out = JSON.parse(globalThis.vukaTranspile(JSON.stringify(data.files)))
      out.ms = performance.now() - start
    } else if (op === 'format') {
      out = JSON.parse(globalThis.vukaFormat(data.path, data.src))
    } else {
      throw new Error(`unknown op ${op}`)
    }
    postMessage({ id, ok: true, out })
  } catch (err) {
    postMessage({ id, ok: false, error: String(err && err.message ? err.message : err) })
  }
}
