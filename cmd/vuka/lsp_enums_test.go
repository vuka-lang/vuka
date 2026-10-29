package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

const enumsOrders = `package orders

// Status is where an order is.
enum Status {
	// Draft is an order being written.
	Draft
	Sent
	Paid
}

// Shape is a figure.
enum Shape {
	// Circle is round.
	Circle{Radius float64}
	Empty
}
`

const enumsMain = `package main

import (
	"fmt"

	"lsptest/orders"
)

func label(s orders.Status) string {
	match s {
	case orders.Status.Draft:
		return "draft"
	case _:
		return "other"
	}
}

func main() {
	fmt.Println(label(orders.Status.Draft), orders.Shape.Empty)
}
`

// TestLSPEnums: after an enum's name, completion lists its variants (and a
// value enum's Values and Parse), hover shows a variant's doc, and a match
// missing variants is a diagnostic at the match.
func TestLSPEnums(t *testing.T) {
	c, dir, _ := startLSPWith(t, map[string]string{"orders/orders.vuka": enumsOrders, "main.vuka": enumsMain})
	uri := pathToURI(filepath.Join(dir, "main.vuka"))
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{
		"uri": uri, "languageId": "vuka", "version": 1, "text": enumsMain}})
	c.waitDiags(uri, func(ds []any) bool { return len(ds) == 0 })

	complete := func(src, after string, version int) map[string]map[string]any {
		t.Helper()
		c.notify("textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": uri, "version": version},
			"contentChanges": []any{map[string]any{"text": src}}})
		off := strings.Index(src, after) + len(after)
		return completionLabels(t, c.call("textDocument/completion", map[string]any{"textDocument": map[string]any{"uri": uri},
			"position": positionOf([]byte(src), off)}))
	}

	t.Run("completion on a value enum", func(t *testing.T) {
		src := strings.Replace(enumsMain, "fmt.Println(label(orders.Status.Draft), orders.Shape.Empty)", "x := orders.Status.\n\tfmt.Println(x)", 1)
		items := complete(src, "orders.Status.", 2)
		for _, want := range []string{"Draft", "Sent", "Paid", "Values", "Parse"} {
			if items[want] == nil {
				b, _ := json.Marshal(items)
				t.Fatalf("no %s completion: %.900s", want, b)
			}
		}
		if d, _ := items["Values"]["detail"].(string); d != "static func() []orders.Status" {
			t.Errorf("Values detail = %q", d)
		}
		if doc, _ := json.Marshal(items["Draft"]["documentation"]); !strings.Contains(string(doc), "an order being written") {
			t.Errorf("Draft documentation = %s", doc)
		}
	})

	t.Run("completion on an enum with data", func(t *testing.T) {
		src := strings.Replace(enumsMain, "fmt.Println(label(orders.Status.Draft), orders.Shape.Empty)", "x := orders.Shape.\n\tfmt.Println(x)", 1)
		items := complete(src, "orders.Shape.", 3)
		if items["Circle"] == nil || items["Empty"] == nil {
			b, _ := json.Marshal(items)
			t.Fatalf("no variant completions: %.900s", b)
		}
		if items["Values"] != nil {
			t.Errorf("an enum with data has no Values")
		}
	})

	t.Run("hover on a variant", func(t *testing.T) {
		c.notify("textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": uri, "version": 4},
			"contentChanges": []any{map[string]any{"text": enumsMain}}})
		c.waitDiags(uri, func(ds []any) bool { return len(ds) == 0 })
		off := strings.Index(enumsMain, "Status.Draft), orders") + len("Status.Dr")
		v := c.call("textDocument/hover", map[string]any{"textDocument": map[string]any{"uri": uri},
			"position": positionOf([]byte(enumsMain), off)})
		b, _ := json.Marshal(v)
		if !strings.Contains(string(b), "orders.Status.Draft") || !strings.Contains(string(b), "an order being written") {
			t.Fatalf("hover: %s", b)
		}
	})

	t.Run("a match missing variants", func(t *testing.T) {
		src := strings.Replace(enumsMain, "\tcase _:\n\t\treturn \"other\"\n", "\tcase orders.Status.Sent:\n\t\treturn \"sent\"\n", 1)
		c.notify("textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": uri, "version": 5},
			"contentChanges": []any{map[string]any{"text": src}}})
		ds := c.waitDiags(uri, func(ds []any) bool { return len(ds) > 0 })
		b, _ := json.Marshal(ds)
		if !strings.Contains(string(b), "missing orders.Status.Paid") || !strings.Contains(string(b), `"line":9`) {
			t.Fatalf("diagnostics: %s", b)
		}
	})
}

const enumMethodsMain = `package main

import (
	"errors"
	"fmt"
)

// PasswordError is why a password change was refused.
enum PasswordError {
	CurrentRequired
	Mismatch
	ChangeFailed
}

func (PasswordError.CurrentRequired) Error() string { return "Current password is required" }
func (e PasswordError.Mismatch) Error() string      { return "Passwords do not match" }

// Error is the message of the variants without their own.
func (e PasswordError) Error() string { return "Failed to change password" }

func main() {
	var err error = PasswordError.Mismatch
	fmt.Println(errors.Is(err, PasswordError.Mismatch), PasswordError.ChangeFailed.Error())
}
`

// TestLSPValueEnumMethods: hover on a value enum's variant method says
// what it is; hover on a call shows the enum's one method, and definition
// from the call goes to every declaration of it.
func TestLSPValueEnumMethods(t *testing.T) {
	c, dir, _ := startLSPWith(t, map[string]string{"main.vuka": enumMethodsMain})
	uri := pathToURI(filepath.Join(dir, "main.vuka"))
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{
		"uri": uri, "languageId": "vuka", "version": 1, "text": enumMethodsMain}})
	c.waitDiags(uri, func(ds []any) bool { return len(ds) == 0 })
	at := func(needle string, delta int) map[string]any {
		return map[string]any{"textDocument": map[string]any{"uri": uri},
			"position": positionOf([]byte(enumMethodsMain), strings.Index(enumMethodsMain, needle)+delta)}
	}
	line := func(needle string) float64 {
		return float64(strings.Count(enumMethodsMain[:strings.Index(enumMethodsMain, needle)], "\n"))
	}

	t.Run("hover on a variant method", func(t *testing.T) {
		b, _ := json.Marshal(c.call("textDocument/hover", at("PasswordError.Mismatch) Error", len("PasswordError.Mismatch) Er"))))
		if !strings.Contains(string(b), "func (e PasswordError.Mismatch) Error() string") || !strings.Contains(string(b), "switches on the value") {
			t.Fatalf("hover: %s", b)
		}
	})
	t.Run("hover on the default", func(t *testing.T) {
		b, _ := json.Marshal(c.call("textDocument/hover", at("(e PasswordError) Error", len("(e PasswordError) Er"))))
		if !strings.Contains(string(b), "The default of `PasswordError.Error`") {
			t.Fatalf("hover: %s", b)
		}
	})
	t.Run("hover on a call", func(t *testing.T) {
		b, _ := json.Marshal(c.call("textDocument/hover", at("ChangeFailed.Error()", len("ChangeFailed.Er"))))
		if !strings.Contains(string(b), "func (p PasswordError) Error() string") {
			t.Fatalf("hover: %s", b)
		}
	})
	t.Run("definition from a call", func(t *testing.T) {
		v := c.call("textDocument/definition", at("ChangeFailed.Error()", len("ChangeFailed.Er")))
		b, _ := json.Marshal(v)
		list, _ := v.([]any)
		var lines []float64
		for _, l := range list {
			m, _ := l.(map[string]any)
			r, _ := m["range"].(map[string]any)
			s, _ := r["start"].(map[string]any)
			n, _ := s["line"].(float64)
			lines = append(lines, n)
		}
		want := []float64{line("(PasswordError.CurrentRequired) Error"), line("(e PasswordError.Mismatch) Error"), line("(e PasswordError) Error")}
		if len(lines) != len(want) {
			t.Fatalf("definition: %s", b)
		}
		for i := range want {
			if lines[i] != want[i] {
				t.Fatalf("definition: %s, want lines %v", b, want)
			}
		}
	})
}
