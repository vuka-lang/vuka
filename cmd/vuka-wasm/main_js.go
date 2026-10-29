//go:build js && wasm

package main

import (
	"encoding/json"
	"fmt"
	"syscall/js"

	"github.com/vuka-lang/vuka/v2/diag"
)

// main registers the playground's functions for the page's worker:
//
//	vukaLoad(snapshot: Uint8Array): string | null   (an error)
//	vukaTranspile(files: string /* JSON {path: source} */): string /* JSON result */
//	vukaFormat(path: string, src: string): string /* JSON {src} or {error} */
func main() {
	var pg *playground
	js.Global().Set("vukaLoad", js.FuncOf(func(_ js.Value, args []js.Value) any {
		b := make([]byte, args[0].Get("length").Int())
		js.CopyBytesToGo(b, args[0])
		pkgs, err := decodeSnapshot(b)
		if err != nil {
			return err.Error()
		}
		pg = newPlayground(pkgs)
		return nil
	}))
	js.Global().Set("vukaTranspile", js.FuncOf(func(_ js.Value, args []js.Value) (out any) {
		res := result{Files: map[string]string{}, Diagnostics: []diag.Diagnostic{}}
		defer func() {
			if r := recover(); r != nil {
				res.Diagnostics = append(res.Diagnostics, diag.Diagnostic{Code: "E0001", Message: fmt.Sprint("the compiler panicked: ", r)})
			}
			b, _ := json.Marshal(res)
			out = string(b)
		}()
		var files map[string]string
		if err := json.Unmarshal([]byte(args[0].String()), &files); err != nil {
			panic(err)
		}
		if pg == nil {
			panic("vukaLoad first")
		}
		res = pg.transpile(files)
		return nil
	}))
	js.Global().Set("vukaFormat", js.FuncOf(func(_ js.Value, args []js.Value) (out any) {
		var f formatted
		defer func() {
			if r := recover(); r != nil {
				f = formatted{Error: &diag.Diagnostic{Code: "E0001", Message: fmt.Sprint("the formatter panicked: ", r)}}
			}
			b, _ := json.Marshal(f)
			out = string(b)
		}()
		f = formatFile(args[0].String(), args[1].String())
		return nil
	}))
	select {}
}
