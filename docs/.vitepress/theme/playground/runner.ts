// A runner is what the playground hands a project to. wasmRunner transpiles
// in the browser (the vuka.wasm worker); remoteRunner adds run, which builds
// and starts the project on the runner service and serves the program from
// a URL of its own.

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

export interface Ran {
  session?: string
  url?: string // where the program serves, when it does
  diagnostics: Diagnostic[]
  buildLog: string
  output?: string // what a program that exited at once printed
  error?: string
}

export interface RunEvents {
  log?(line: string): void // the build's output, as it comes
  output?(line: string): void // the program's
  exit?(status: string): void
}

export interface Runner {
  transpile(files: Record<string, string>): Promise<Transpiled>
  format(path: string, src: string): Promise<Formatted>
  run?(files: Record<string, string>, on: RunEvents, signal: AbortSignal): Promise<Ran>
  stop?(session: string): void
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

// remoteRunner transpiles and formats with inner and runs on the runner
// service at api: POST /run streams the build as server-sent events, then
// the result, then the program's output; DELETE /s/<session> stops it.
export function remoteRunner(inner: Runner, api: string): Runner {
  api = api.replace(/\/+$/, '')
  return {
    ...inner,
    async run(files, on, signal) {
      const res = await fetch(api + '/run', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', Accept: 'text/event-stream' },
        body: JSON.stringify({ files }),
        signal,
      })
      if (!res.ok || !res.body) {
        const body = await res.json().catch(() => ({}))
        throw new Error(body.error || `the runner answered ${res.status}`)
      }
      const reader = res.body.pipeThrough(new TextDecoderStream()).getReader()
      return new Promise<Ran>((resolve, reject) => {
        let buf = ''
        let done = false
        const event = (block: string) => {
          let name = 'message'
          let data = ''
          for (const line of block.split('\n')) {
            if (line.startsWith('event: ')) name = line.slice(7)
            else if (line.startsWith('data: ')) data += line.slice(6)
          }
          if (!data) return
          const v = JSON.parse(data)
          if (name === 'log') on.log?.(v)
          else if (name === 'output') on.output?.(v)
          else if (name === 'exit') on.exit?.(v.status)
          else if (name === 'result') {
            done = true
            resolve(v)
          }
        }
        ;(async () => {
          try {
            for (;;) {
              const { value, done: end } = await reader.read()
              if (end) break
              buf += value
              let i
              while ((i = buf.indexOf('\n\n')) >= 0) {
                event(buf.slice(0, i))
                buf = buf.slice(i + 2)
              }
            }
            if (!done) reject(new Error('the runner closed the connection'))
          } catch (err) {
            if (!done) reject(err)
          }
        })()
      })
    },
    stop(session) {
      fetch(`${api}/s/${session}`, { method: 'DELETE', keepalive: true }).catch(() => {})
    },
  }
}
