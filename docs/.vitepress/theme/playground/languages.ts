// Highlighting for the playground's editors: Go (generated code and .go
// files), .vuka (Go's modes plus Vuka's keywords) and .vuka.html, whose
// markup holds Go in its <script lang="go">, its {holes} and its statement
// lines — the regions editors/vscode/syntaxes/vuka-html.tmLanguage.json
// marks.
import { HighlightStyle, StreamLanguage, syntaxHighlighting, type StreamParser } from '@codemirror/language'
import { go as goLegacy } from '@codemirror/legacy-modes/mode/go'
import { go } from '@codemirror/lang-go'
import { tags as t } from '@lezer/highlight'
import type { Extension } from '@codemirror/state'

const vukaWords = new Set(['enum', 'match', 'Ok', 'Err', 'Some', 'None'])

function goToken(stream: any, state: any): string | null {
  const style = goLegacy.token(stream, state)
  if (style === 'variable' && vukaWords.has(stream.current())) return 'keyword'
  return style
}

const vuka = StreamLanguage.define<any>({
  name: 'vuka',
  startState: () => goLegacy.startState!(4),
  token: goToken,
  indent: goLegacy.indent,
  languageData: { commentTokens: { line: '//' } },
})

type Mode = 'markup' | 'script' | 'raw' | 'tag' | 'hole' | 'stmt' | 'comment' | 'gocomment'

interface HTMLState {
  mode: Mode
  back: Mode // where a hole returns
  depth: number
  raw: string // the closing tag of a raw element
  go: any
}

const statement = /^\s*(?:(?:\}\s*)?(?:if|for|switch|else|match)\b|case\b|default\s*:|[A-Za-z_][\w.,\s]*:?=[^=])/

const vukaHTMLParser: StreamParser<HTMLState> = {
  name: 'vuka-html',
  startState: () => ({ mode: 'markup', back: 'markup', depth: 0, raw: '', go: goLegacy.startState!(4) }),
  copyState: (s) => ({ ...s, go: { ...s.go, context: s.go.context && { ...s.go.context } } }),
  token(stream, s) {
    if (s.mode === 'stmt' && stream.sol()) s.mode = 'markup'
    switch (s.mode) {
      case 'script':
        if (stream.match(/^<\/script\s*>/)) {
          s.mode = 'markup'
          return 'tagName'
        }
        return goToken(stream, s.go)
      case 'raw':
        if (stream.match(s.raw)) {
          s.mode = 'markup'
          return 'tagName'
        }
        stream.next()
        return null
      case 'comment':
        if (stream.skipTo('-->')) stream.match('-->'), (s.mode = 'markup')
        else stream.skipToEnd()
        return 'comment'
      case 'gocomment':
        if (stream.skipTo('*/}')) stream.match('*/}'), (s.mode = 'markup')
        else stream.skipToEnd()
        return 'comment'
      case 'hole':
        if (stream.eat('{')) {
          s.depth++
          return 'brace'
        }
        if (stream.eat('}')) {
          if (--s.depth === 0) s.mode = s.back
          return 'brace'
        }
        return goToken(stream, s.go)
      case 'stmt':
        return goToken(stream, s.go)
      case 'tag':
        if (stream.eatSpace()) return null
        if (stream.match('/>') || stream.eat('>')) {
          s.mode = 'markup'
          return 'angleBracket'
        }
        if (stream.eat('{')) {
          s.mode = 'hole'
          s.back = 'tag'
          s.depth = 1
          return 'brace'
        }
        if (stream.match(/^"[^"]*"?/) || stream.match(/^'[^']*'?/)) return 'string'
        if (stream.eat('=')) return 'operator'
        if (stream.match(/^[^\s=>/{"']+/)) return 'attributeName'
        stream.next()
        return null
    }
    if (stream.sol() && stream.match(/^\s*\}\s*$/, false)) {
      stream.skipToEnd()
      return 'brace'
    }
    if (stream.sol() && stream.match(statement, false)) {
      s.mode = 'stmt'
      return goToken(stream, s.go)
    }
    if (stream.match('<!--')) {
      s.mode = 'comment'
      return 'comment'
    }
    if (stream.match('{/*')) {
      s.mode = 'gocomment'
      return 'comment'
    }
    if (stream.match('{{')) return 'escape'
    if (stream.match(/^<script\b[^>]*\blang=["']go["'][^>]*>/)) {
      s.mode = 'script'
      return 'tagName'
    }
    const raw = stream.match(/^<(script|style)\b[^>]*>/) as RegExpMatchArray | null
    if (raw) {
      s.mode = 'raw'
      s.raw = `</${raw[1]}>`
      return 'tagName'
    }
    if (stream.eat('{')) {
      s.mode = 'hole'
      s.back = 'markup'
      s.depth = 1
      return 'brace'
    }
    if (stream.match(/^<\/?[A-Za-z][\w.:-]*/)) {
      s.mode = 'tag'
      return 'tagName'
    }
    stream.next()
    while (!stream.eol() && !/[<{]/.test(stream.peek()!)) stream.next()
    return null
  },
  languageData: { commentTokens: { block: { open: '<!--', close: '-->' } } },
}

const vukaHTML = StreamLanguage.define(vukaHTMLParser)

const style = HighlightStyle.define([
  { tag: [t.keyword, t.controlKeyword, t.definitionKeyword, t.moduleKeyword, t.operatorKeyword, t.modifier], color: 'var(--pg-keyword)' },
  { tag: [t.string, t.special(t.string), t.character], color: 'var(--pg-string)' },
  { tag: [t.comment, t.lineComment, t.blockComment, t.docComment], color: 'var(--pg-comment)', fontStyle: 'italic' },
  { tag: [t.number, t.bool, t.null, t.atom], color: 'var(--pg-number)' },
  { tag: [t.typeName, t.standard(t.typeName), t.className], color: 'var(--pg-type)' },
  { tag: [t.function(t.variableName), t.function(t.definition(t.variableName)), t.function(t.propertyName)], color: 'var(--pg-func)' },
  { tag: [t.tagName, t.angleBracket], color: 'var(--pg-tag)' },
  { tag: t.attributeName, color: 'var(--pg-attr)' },
  { tag: [t.brace, t.escape], color: 'var(--pg-brace)', fontWeight: '600' },
  { tag: t.standard(t.variableName), color: 'var(--pg-builtin)' },
])

export const highlighting = syntaxHighlighting(style)

// language is the highlighting of a file by its path.
export function language(path: string): Extension {
  if (path.endsWith('.vuka.html')) return vukaHTML
  if (path.endsWith('.vuka')) return vuka
  if (path.endsWith('.go')) return go()
  return []
}

export const goLanguage = go()
