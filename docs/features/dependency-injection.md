# Dependency injection

Services use ordinary constructors in `.go` or `.vuka` logic files. The app
registers them explicitly with `app.Service(NewX)` before Build or Run.
Constructor parameters identify dependencies and construction failures stop startup.

```go
package services

type Config struct { Prefix string }
type Greeter struct { config *Config }

func NewConfig() *Config { return &Config{Prefix: "Hello"} }
func NewGreeter(c *Config) *Greeter { return &Greeter{config: c} }
func (g *Greeter) Greet(name string) string { return g.config.Prefix + ", " + name }
```

Register both constructors on the app. Handler and decorated hook parameters
are filled by type: context, form struct, HTTP request, signed-in identity and
registered services. A shape's unexported field of a provided type is injected;
other unexported fields are tracked server state.

See [Web dependency injection](/web/dependency-injection) for binding and lifecycle.
