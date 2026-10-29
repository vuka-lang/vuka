// A shared project is its files and the open one, as JSON, deflated and
// base64url'd into the URL's hash: #p=….

export interface Project {
  files: Record<string, string>
  open: string
}

async function pipe(bytes: Uint8Array, stream: CompressionStream | DecompressionStream): Promise<Uint8Array> {
  const out = new Blob([bytes]).stream().pipeThrough(stream)
  return new Uint8Array(await new Response(out).arrayBuffer())
}

function base64url(bytes: Uint8Array): string {
  let s = ''
  for (let i = 0; i < bytes.length; i += 0x8000) s += String.fromCharCode(...bytes.subarray(i, i + 0x8000))
  return btoa(s).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '')
}

function unbase64url(s: string): Uint8Array {
  const bin = atob(s.replace(/-/g, '+').replace(/_/g, '/'))
  return Uint8Array.from(bin, (c) => c.charCodeAt(0))
}

export async function encodeProject(p: Project): Promise<string> {
  const json = new TextEncoder().encode(JSON.stringify({ f: p.files, o: p.open }))
  return 'p=' + base64url(await pipe(json, new CompressionStream('deflate-raw')))
}

// decodeProject is the project in a hash; null when it holds none.
export async function decodeProject(hash: string): Promise<Project | null> {
  const m = /^#?p=([\w-]+)$/.exec(hash)
  if (!m) return null
  try {
    const json = new TextDecoder().decode(await pipe(unbase64url(m[1]), new DecompressionStream('deflate-raw')))
    const { f, o } = JSON.parse(json)
    if (typeof f !== 'object' || !f || Object.values(f).some((v) => typeof v !== 'string')) return null
    const files = f as Record<string, string>
    return { files, open: typeof o === 'string' && o in files ? o : Object.keys(files)[0] ?? '' }
  } catch {
    return null
  }
}
