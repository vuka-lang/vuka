# Overloading

**Go:** one name, one function — `AreaCircle`, `AreaRect`.

**Vuka adds** functions and methods that share a name and differ in their
parameters:

```vuka
func area(c Circle) float64 { return math.Pi * c.R * c.R }
func area(r Rect) float64   { return r.W * r.H }

func (c *Counter) Add(by int)    { c.n += by }
func (c *Counter) Add(by string) { c.n += len(by) }
func (c *Counter) Add(a, b int)  { c.n += a + b }

area(Circle{R: 1})
c.Add(2, 3)
```

Each call goes to the overload that best fits its arguments' static types:
identical types first, then an untyped constant's default type (`scale(2)` picks
`int`, `scale(2.5)` picks `float64`), then anything assignable. No match, or a
tie, is an error listing the candidates.

## What it becomes

Each overload is named after its parameter types, the way Go names such
functions, and each call is pointed at one:

::: vuka
```vuka
func area(c Circle) float64 { return math.Pi * c.R * c.R }
func area(r Rect) float64   { return r.W * r.H }

func (c *Counter) Add(by int)    { c.n += by }
func (c *Counter) Add(a, b int)  { c.n += a + b }
func (c *Counter) Add(xs ...int) { c.n += len(xs) }

area(Circle{R: 1})
c.Add(2, 3)
```
:::

::: go
```go
func areaCircle(c Circle) float64 { return math.Pi * c.R * c.R }
func areaRect(r Rect) float64     { return r.W * r.H }

func (c *Counter) AddInt(by int)      { c.n += by }
func (c *Counter) AddIntInt(a, b int) { c.n += a + b }
func (c *Counter) AddInts(xs ...int)  { c.n += len(xs) }

areaCircle(Circle{R: 1})
c.AddIntInt(2, 3)
```
:::

A parameter's type is written as part of the name: `int` is `Int`,
`*Shape` `Shape`, `time.Time` `Time`, `[]Order` and `...Order` `Orders`,
`map[string]int` `MapStringInt`, `any` `Any`; a leading `context.Context`
is left out when more follows (`Update(ctx, t Typing)` is `UpdateTyping`),
and an overload taking nothing is `area0`. A name the package already has,
or that two overloads would share (`total(o Order)` and `total(o *Order)`),
gets a number: `totalOrder2`. A live component's `Update` overloads are
the methods the session delivers messages to (`UpdateNewMessage`,
[Stateful components](/ui/stateful#messages)). Vuka 0.x named overloads
`area__Circle`; `ui.IsUpdate` still takes `Update__T`.

To give Go code a name of your own for one overload, use `@export`:

```vuka
@export("RectArea")
func area(r Rect) float64 { return r.W * r.H }
```

An overloaded method can't satisfy an interface method of the plain name, and
generic functions can't be overloaded yet.
