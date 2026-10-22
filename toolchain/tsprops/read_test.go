package tsprops

import (
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"testing"
)

// sig is a compact rendering of a component's props for the tables:
// name?:type with ~ for a model, ! for an event, and +children.
func sig(c *Component) string {
	var parts []string
	for _, p := range c.Props {
		s := p.Name
		if p.Optional {
			s += "?"
		}
		if p.Model {
			s += "~"
		}
		if p.Event {
			s += "!"
		}
		parts = append(parts, s+":"+show(p.Type))
	}
	if c.Children {
		parts = append(parts, "+children")
	}
	return strings.Join(parts, " ")
}

func show(t *Type) string {
	switch t.Kind {
	case Object:
		var fs []string
		for _, f := range t.Fields {
			o := ""
			if f.Optional {
				o = "?"
			}
			fs = append(fs, f.Name+o+":"+show(f.Type))
		}
		n := ""
		if t.Name != "" {
			n = t.Name
		}
		return n + "{" + strings.Join(fs, ",") + "}"
	case Array:
		return show(t.Elem) + "[]"
	case Record:
		return "Record<" + show(t.Elem) + ">"
	case Union:
		var us []string
		for _, u := range t.Union {
			us = append(us, show(u))
		}
		n := ""
		if t.Name != "" {
			n = t.Name + "="
		}
		return n + strings.Join(us, "|")
	case Func:
		var ps []string
		for _, p := range t.Params {
			ps = append(ps, p.Name+":"+show(p.Type))
		}
		return "(" + strings.Join(ps, ",") + ")=>"
	case Node:
		return "node"
	case Unknown:
		return "?" + t.Text
	}
	return t.String()
}

var files = map[string]string{
	"islands/types.ts": `export interface Point { x: number; y: number }
export type Kind = 'bar' | 'line'`,
}

func readFile(p string) ([]byte, error) {
	if s, ok := files[p]; ok {
		return []byte(s), nil
	}
	return nil, fs.ErrNotExist
}

func TestRead(t *testing.T) {
	tests := []struct{ file, src, want string }{
		{"islands/A.vue", `<script setup lang="ts">
const props = defineProps<{ count: number; label?: string }>()
</script>
<template><button>{{ count }}<slot/></button></template>`,
			"count:number label?:string +children"},
		{"islands/A.vue", `<script setup lang="ts">
interface Props { title: string; size?: 'sm' | 'lg'; tags: string[]; meta: Record<string, number>; at: Date | null }
withDefaults(defineProps<Props>(), { title: 'x', tags: () => [] })
</script>`,
			"title?:string size?:'sm'|'lg' tags?:string[] meta:Record<number> at:Date|null"},
		{"islands/A.vue", `<script setup lang="ts">
const { n = 1, m } = defineProps<{ n: number; m: boolean }>()
const v = defineModel<string>('value')
const open = defineModel<boolean>({ required: true })
const emit = defineEmits<{ select: [day: string]; 'row-click': [row: { id: number }]; 'update:value': [string] }>()
</script>`,
			"n?:number m:boolean value?~:string modelValue~:boolean onSelect?!:(day:string)=> onRowClick?!:(row:{id:number})=>"},
		{"islands/A.vue", `<script setup lang="ts">
const emit = defineEmits<{ (e: 'close'): void; (e: 'pick', n: number): void }>()
defineProps<{}>()
</script>`,
			"onClose?!:()=> onPick?!:(n:number)=>"},
		{"islands/A.vue", `<script setup lang="ts">
import type { Point, Kind } from './types'
type Series = { name: string; points: Point[] }
defineProps<{ series: Series[]; kind: Kind; nested: { a: { b: number } } }>()
</script>`,
			"series:Series{name:string,points:Point{x:number,y:number}[]}[] kind:Kind='bar'|'line' nested:{a:{b:number}}"},
		{"islands/C.tsx", `import React from 'react'
type Props = { label: string; onPick?: (n: number) => void; children?: React.ReactNode }
export default function C({ label, onPick, children }: Props) { return <div>{label}don't</div> }`,
			"label:string onPick?:(n:number)=> +children"},
		{"islands/C.tsx", `interface Props { a: number }
const C: React.FC<Props> = ({ a = 2 }) => <i>{a}</i>
export default C`,
			"a:number"},
		{"islands/C.tsx", `import { forwardRef } from 'react'
interface Props { value: string; setValue?: (v: string) => void }
export default forwardRef<HTMLInputElement, Props>((props, ref) => <input ref={ref} />)`,
			"value:string setValue?:(v:string)=>"},
		{"islands/C.tsx", `interface Props { n: number }
function C(props: Props) { return null }
export default C`,
			"n:number"},
		{"islands/C.tsx", `export default function C({ step = 1 }: { step?: number; when: Date }) { return null }`,
			"step?:number when:Date"},
		{"islands/S.svelte", `<script lang="ts">
  import type { Snippet } from 'svelte'
  interface Props { a: string; b?: number; value: string; children?: Snippet; onchange?: (v: string) => void }
  let { a, b = 1, value = $bindable(''), children, onchange }: Props = $props()
</script>
<p>{a}</p>`,
			"a:string b?:number value?~:string onchange?:(v:string)=> +children"},
		{"islands/S.svelte", `<script lang="ts">
  let { open = $bindable() }: { open: boolean } = $props()
</script>`,
			"open~:boolean"},
		{"islands/S.svelte", `<script lang="ts">
  export let name: string
  export let size: number = 3
</script>`,
			"name:string size?:number"},
		{"islands/V.ts", `interface Props { start: number; label?: string }
export function mount(el: HTMLElement, props: Props, ctx: any) { return {} }`,
			"start:number label?:string"},
		{"islands/V.ts", `export const mount = (el: HTMLElement, props: { n: number }) => ({})`,
			"n:number"},
		{"islands/V.ts", `export function mount(el: HTMLElement) { const r = /'"/; return {} }`,
			""},
		{"islands/A.vue", `<script setup lang="ts">
interface Base { id: number }
interface Props extends Base { name: string }
defineProps<Props & { extra?: boolean }>()
</script>`,
			"id:number name:string extra?:boolean"},
		{"islands/A.vue", `<script setup lang="ts">
interface Box<T> { value: T }
defineProps<{ box: Box<string>; opt: Partial<{ a: number }> }>()
</script>`,
			"box:Box{value:string} opt:{a?:number}"},
	}
	for i, tt := range tests {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			c, err := Read(tt.file, []byte(tt.src), readFile)
			if err != nil {
				t.Fatal(err)
			}
			if got := sig(c); got != tt.want {
				t.Errorf("got  %s\nwant %s", got, tt.want)
			}
		})
	}
}

func TestReadErrors(t *testing.T) {
	tests := []struct{ file, src, want string }{
		{"A.vue", "<script setup>\ndefineProps({ a: Number })\n</script>", "2: defineProps declares runtime props"},
		{"A.vue", "<script setup lang=\"ts\">\ndefineProps<Props>()\n</script>", "2: the props type Props isn't an object type"},
		{"A.vue", "<script setup lang=\"ts\">\nconst m = defineModel('x')\n</script>", "2: defineModel(\"x\") needs its type"},
		{"C.jsx", "export default function C(props) { return null }", "the props parameter has no type"},
		{"C.tsx", "export function C() {}", "no default export"},
		{"S.svelte", "<script>\nlet { a } = $props()\n</script>", "2: $props() has no type"},
		{"V.ts", "export function start() {}", "no export function mount"},
	}
	for _, tt := range tests {
		_, err := Read(tt.file, []byte(tt.src), readFile)
		var e *Error
		if err == nil || !errors.As(err, &e) || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s: got %v, want %q", tt.src, err, tt.want)
		}
	}
}
