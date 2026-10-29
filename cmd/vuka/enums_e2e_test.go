package main

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const enumsOrdersPkg = `package orders

// Status is where an order is.
enum Status { Draft, Sent, Paid }

// Role is what a member of staff may do.
enum Role string {
	Admin = "admin"
	Clerk = "clerk"
}

// Shape is a parcel's footprint.
enum Shape {
	Circle{Radius float64}
	Rect{W, H float64}
	Unknown
}

// Area is a function: an enum with data is an interface, which has no methods of its own.
func Area(s Shape) float64 {
	match s {
	case Shape.Circle{Radius: r}:
		return 3 * r * r
	case Shape.Rect{W: w, H: h}:
		return w * h
	case Shape.Unknown:
		return 0
	}
}
`

const enumsOrdersTest = `package orders

import (
	"encoding/json"
	"testing"
)

type order struct {
	ID     int
	Status Status
	Role   Role
}

func TestValueEnum(t *testing.T) {
	if Status.Sent.String() != "Sent" || Status(9).String() != "Status(9)" {
		t.Errorf("String: %s %s", Status.Sent, Status(9))
	}
	if got := Status.Values(); len(got) != 3 || got[0] != Status.Draft || got[2] != Status.Paid {
		t.Errorf("Values: %v", got)
	}
	if !Status.Paid.Valid() || Status(3).Valid() {
		t.Errorf("Valid")
	}
	match Status.Parse("Paid") {
	case Ok(s):
		if s != Status.Paid {
			t.Errorf("Parse: %v", s)
		}
	case Err(e):
		t.Errorf("Parse: %v", e)
	}
	if Status.Parse("paid").IsOk() {
		t.Errorf("Parse is case-sensitive")
	}
	if Role.Admin.String() != "admin" || Role.Parse("clerk").Unwrap() != Role.Clerk || Role.Parse("Clerk").Unwrap() != Role.Clerk {
		t.Errorf("string enum")
	}
}

func TestJSON(t *testing.T) {
	b, err := json.Marshal(order{ID: 7, Status: Status.Sent, Role: Role.Clerk})
	if err != nil || string(b) != ` + "`" + `{"ID":7,"Status":"Sent","Role":"clerk"}` + "`" + ` {
		t.Fatalf("Marshal: %s %v", b, err)
	}
	var o order
	if err := json.Unmarshal(b, &o); err != nil || o.Status != Status.Sent || o.Role != Role.Clerk {
		t.Fatalf("Unmarshal: %+v %v", o, err)
	}
	if err := json.Unmarshal([]byte(` + "`" + `{"Status":"Lost"}` + "`" + `), &o); err == nil {
		t.Errorf("an unknown name unmarshals")
	}
	if _, err := json.Marshal(order{Status: 5, Role: Role.Admin}); err == nil {
		t.Errorf("an invalid value marshals")
	}
}

func TestSQL(t *testing.T) {
	var s Status
	for _, src := range []any{int64(2), "Sent", []byte("Draft")} {
		if err := s.Scan(src); err != nil {
			t.Errorf("Scan(%v): %v", src, err)
		}
	}
	if s != Status.Draft {
		t.Errorf("Scan: %v", s)
	}
	if s.Scan(int64(8)) == nil || s.Scan(2.5) == nil {
		t.Errorf("Scan accepts an invalid value")
	}
	if v, err := Status.Paid.Value(); v != int64(2) || err != nil {
		t.Errorf("Value: %v %v", v, err)
	}
	var r Role
	if err := r.Scan("admin"); err != nil || r != Role.Admin {
		t.Errorf("Role.Scan: %v %v", r, err)
	}
	if v, _ := Role.Clerk.Value(); v != "clerk" {
		t.Errorf("Role.Value: %v", v)
	}
}

func TestEnumWithData(t *testing.T) {
	shapes := []Shape{Shape.Circle{Radius: 1}, Shape.Rect{W: 2, H: 3}, Shape.Unknown}
	var sum float64
	for _, s := range shapes {
		sum += Area(s)
	}
	if sum != 9 {
		t.Errorf("areas: %v", sum)
	}
}
`

const enumsPlainGo = `package orders

// FirstStatus is plain Go using the enum's Go names.
func FirstStatus() Status {
	s, err := ParseStatus("Draft").Get()
	if err != nil || s != StatusDraft || len(StatusValues()) != 3 {
		panic("plain Go")
	}
	var shape Shape = ShapeRect{W: 1, H: 1}
	_ = shape
	return s
}
`

const enumsReport = `package report

import (
	"fmt"

	"example.com/shop/orders"
)

func Label(s orders.Status) string {
	match s {
	case orders.Status.Draft:
		return "draft"
	case orders.Status.Sent, orders.Status.Paid:
		return "out"
	}
}

func Shape(s orders.Shape) string {
	match s {
	case orders.Shape.Circle{Radius: r} if r > 10:
		return "big circle"
	case orders.Shape.Circle{}:
		return "circle"
	case orders.Shape.Rect{W: w, H: h}:
		return fmt.Sprint("rect ", w*h)
	case orders.Shape.Unknown:
		return "unknown"
	}
}

func Summary() string {
	return fmt.Sprint(Label(orders.Status.Paid), " ", Shape(orders.Shape.Rect{W: 2, H: 2}), " ", orders.Status.Values(), " ", orders.Role.Admin)
}
`

const enumsLegacy = `package legacy

import "example.com/shop/orders"

// Paid is plain Go in another package using the enum.
func Paid() string {
	var s orders.Status
	if err := s.UnmarshalText([]byte("Paid")); err != nil || s != orders.StatusPaid {
		panic(err)
	}
	return s.String() + " " + orders.ShapeCircle{Radius: 1}.Area2()
}
`

// TestEnums builds a module whose enums are used from another Vuka package
// and from plain Go, and runs its .vuka tests of every generated method.
func TestEnums(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the vuka command and runs the go command")
	}
	vuka := buildVuka(t)
	repo, _ := filepath.Abs("../..")
	root := tempModule(t, map[string]string{
		"go.mod":                  "module example.com/shop\n\ngo 1.23\n\nrequire github.com/vuka-lang/vuka/v2 v2.0.0\n\nreplace github.com/vuka-lang/vuka/v2 => " + repo + "\n",
		"orders/orders.vuka":      enumsOrdersPkg,
		"orders/orders_test.vuka": enumsOrdersTest,
		"orders/plain.go":         enumsPlainGo,
		"orders/area.go":          "package orders\n\nimport \"fmt\"\n\nfunc (c ShapeCircle) Area2() string { return fmt.Sprint(c.Radius * 2) }\n",
		"report/report.vuka":      enumsReport,
		"legacy/legacy.go":        enumsLegacy,
		"main.vuka":               "package main\n\nimport (\n\t\"fmt\"\n\n\t\"example.com/shop/legacy\"\n\t\"example.com/shop/orders\"\n\t\"example.com/shop/report\"\n)\n\nfunc main() {\n\tfmt.Println(report.Summary(), legacy.Paid(), orders.FirstStatus())\n}\n",
	})
	if out, err := runVuka(t, vuka, root, "test", "./..."); err != nil {
		t.Fatalf("vuka test: %v\n%s", err, out)
	}
	if out, err := runVuka(t, vuka, root, "vet", "./..."); err != nil {
		t.Fatalf("vuka vet: %v\n%s", err, out)
	}
	bin := filepath.Join(root, "shop")
	if out, err := runVuka(t, vuka, root, "build", "-o", bin, "."); err != nil {
		t.Fatalf("vuka build: %v\n%s", err, out)
	}
	got, err := exec.Command(bin).Output()
	if want := "out rect 4 [Draft Sent Paid] admin Paid 2 Draft"; err != nil || strings.TrimSpace(string(got)) != want {
		t.Errorf("program printed %q (%v), want %q", got, err, want)
	}

	// A match in another package missing a variant fails the build there.
	writeFile(t, filepath.Join(root, "report", "report.vuka"), strings.Replace(enumsReport, "\tcase orders.Shape.Unknown:\n\t\treturn \"unknown\"\n", "", 1))
	out, err := runVuka(t, vuka, root, "build", "-o", bin, ".")
	if err == nil || !strings.Contains(out, "error[E0113]: match on orders.Shape isn't exhaustive: missing orders.Shape.Unknown") || !strings.Contains(out, "--> report/report.vuka:19:2") {
		t.Errorf("a non-exhaustive match: %v\n%s", err, out)
	}
}

// TestExplainEnums: explain shows an enum's declaration beside its Go, and
// the methods it generates.
func TestExplainEnums(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the go command")
	}
	t.Setenv("GOFLAGS", "-mod=mod")
	t.Setenv("GOWORK", "off")
	repo, _ := filepath.Abs("../..")
	root := tempModule(t, map[string]string{
		"go.mod":             "module example.com/shop\n\ngo 1.23\n\nrequire github.com/vuka-lang/vuka/v2 v2.0.0\n\nreplace github.com/vuka-lang/vuka/v2 => " + repo + "\n",
		"orders/orders.vuka": enumsOrdersPkg,
	})
	var out bytes.Buffer
	if err := explain([]string{filepath.Join(root, "orders", "orders.vuka")}, &out); err != nil {
		t.Fatalf("explain: %v", err)
	}
	for _, want := range []string{
		"vuka │ enum Status { Draft, Sent, Paid }",
		"go   │ type Status int; const ( StatusDraft Status = iota; StatusSent; StatusPaid )",
		"go   │ func ParseStatus(text string) vuka.Result[Status] {",
		"go   │ type Shape interface{ isShape() }; type (",
		"go   │ func (ShapeCircle) isShape() {}",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("explanation lacks %q:\n%s", want, out.String())
		}
	}
}

const enumMethodsPkg = `package main

import (
	"errors"
	"fmt"
)

// PaymentError is why a payment was refused.
enum PaymentError {
	Declined
	Expired
	Unavailable
}

func (PaymentError.Declined) Error() string { return "the card was declined" }
func (e PaymentError.Expired) Error() string { return "the card has expired (" + e.String() + ")" }

// Error is the message of the errors without their own.
func (e PaymentError) Error() string { return "payments are unavailable" }

func pay(card string) error {
	switch card {
	case "declined":
		return PaymentError.Declined
	case "old":
		return PaymentError.Expired
	case "down":
		return PaymentError.Unavailable
	}
	return nil
}

func main() {
	for _, card := range []string{"declined", "old", "down"} {
		err := pay(card)
		fmt.Println(err, errors.Is(err, PaymentError.Expired))
	}
	fmt.Println(PaymentError(7).Error())
}
`

// TestEjectValueEnumMethods: ejected, a value enum's variant methods are
// functions and the enum's one method switches between them, readable Go
// that go build and go vet take and that prints what vuka's build printed.
func TestEjectValueEnumMethods(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the go command")
	}
	root := t.TempDir()
	gomod, gosum := testModule(t, "example.com/app")
	writeFile(t, filepath.Join(root, "go.mod"), gomod)
	writeFile(t, filepath.Join(root, "go.sum"), gosum)
	writeFile(t, filepath.Join(root, "payments", "main.vuka"), enumMethodsPkg)
	t.Setenv("GOWORK", "off")
	t.Setenv("GOFLAGS", "-mod=mod")
	t.Chdir(root)
	bin := t.TempDir()
	var stderr bytes.Buffer
	if err := runGo("build", []string{"-o", filepath.Join(bin, "vuka-built"), "./payments"}, io.Discard, &stderr); err != nil {
		t.Fatalf("vuka build: %v\n%s", err, stderr.Bytes())
	}
	want, err := exec.Command(filepath.Join(bin, "vuka-built")).CombinedOutput()
	if err != nil {
		t.Fatalf("%v\n%s", err, want)
	}
	if !strings.Contains(string(want), "the card has expired (Expired) true") || !strings.Contains(string(want), "payments are unavailable\n") {
		t.Fatalf("vuka's build printed:\n%s", want)
	}
	var out bytes.Buffer
	if err := eject([]string{"-rm", "./..."}, &out); err != nil {
		t.Fatal(err)
	}
	src, err := os.ReadFile(filepath.Join("payments", "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{
		"func paymentErrorDeclinedError() string { return \"the card was declined\" }",
		"func paymentErrorExpiredError(e PaymentError) string {",
		"// defaultPaymentErrorError is the message of the errors without their own.\nfunc defaultPaymentErrorError(e PaymentError) string {",
		"// Error calls the Error of p's variant: PaymentError.Declined, PaymentError.Expired, and the default on PaymentError for the others.\n" +
			"func (p PaymentError) Error() string {\n\tswitch p {\n\tcase PaymentErrorDeclined:\n\t\treturn paymentErrorDeclinedError()\n" +
			"\tcase PaymentErrorExpired:\n\t\treturn paymentErrorExpiredError(p)\n\t}\n\treturn defaultPaymentErrorError(p)\n}\n",
	} {
		if !strings.Contains(string(src), s) {
			t.Errorf("the ejected Go lacks\n%s\n---\n%s", s, src)
		}
	}
	if bytes.Contains(src, []byte("//line ")) {
		t.Errorf("the ejected Go has line directives:\n%s", src)
	}
	for _, args := range [][]string{{"build", "-o", filepath.Join(bin, "go-built"), "./payments"}, {"vet", "./..."}} {
		if out, err := exec.Command("go", args...).CombinedOutput(); err != nil {
			t.Fatalf("go %s after vuka eject -rm: %v\n%s", args[0], err, out)
		}
	}
	got, err := exec.Command(filepath.Join(bin, "go-built")).CombinedOutput()
	if err != nil || !bytes.Equal(got, want) {
		t.Errorf("built with go build: %v\n%s\nwant:\n%s", err, got, want)
	}
}
