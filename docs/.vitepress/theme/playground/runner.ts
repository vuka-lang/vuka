// A runner is what the playground hands a project to. The one runner there is
// transpiles in the browser (the vuka.wasm worker); a runner that also runs
// the project is another implementation of the same interface.

export interface Span {
  file?: string
  line?: number
  col?: number
  endCol?: number
}

export interface Diagnostic {
  code: string
  severity: 'error' | 'warning'
  message: string
  span: Span
  help?: string
  suggest?: string
}

export interface Transpiled {
  files: Record<string, string> // the Go of each source file, by its path
  diagnostics: Diagnostic[]
  ms: number
}

export interface Formatted {
  src?: string
  error?: Diagnostic
}

export interface Runner {
  transpile(files: Record<string, string>): Promise<Transpiled>
  format(path: string, src: string): Promise<Formatted>
  dispose(): void
}

// wasmRunner transpiles with vuka.wasm in a Web Worker, loaded from base
// (the site's public/playground/).
export function wasmRunner(base: string): Runner {
  const worker = new Worker(base + 'worker.js')
  const pending = new Map<number, { resolve: (v: any) => void; reject: (e: Error) => void }>()
  let next = 0
  worker.onmessage = ({ data }) => {
    const p = pending.get(data.id)
    if (!p) return
    pending.delete(data.id)
    data.ok ? p.resolve(data.out) : p.reject(new Error(data.error))
  }
  worker.onerror = (e) => {
    for (const p of pending.values()) p.reject(new Error(e.message || 'the compiler failed to load'))
    pending.clear()
  }
  const call = <T>(msg: object) =>
    new Promise<T>((resolve, reject) => {
      const id = next++
      pending.set(id, { resolve, reject })
      worker.postMessage({ id, ...msg })
    })
  return {
    transpile: (files) => call<Transpiled>({ op: 'transpile', files }),
    format: (path, src) => call<Formatted>({ op: 'format', path, src }),
    dispose: () => worker.terminate(),
  }
}
