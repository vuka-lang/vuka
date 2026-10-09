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

Each overload is renamed after its parameter types — `area__Circle`,
`Add__int_int` — and each call is pointed at one. To give Go code a stable name
for one overload, use `@export`:

```vuka
@export("RectArea")
func area(r Rect) float64 { return r.W * r.H }
```

An overloaded method can't satisfy an interface method of the plain name, and
generic functions can't be overloaded yet.
