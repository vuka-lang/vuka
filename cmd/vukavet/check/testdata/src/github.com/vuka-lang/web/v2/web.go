package web

type RouteOption interface{ Attrs() []any }

type opt struct{}

func (opt) Attrs() []any { return nil }

type Registry struct{}

var Routes = &Registry{}

func (g *Registry) Handle(method, path string, fn any, opts ...RouteOption) *Registry { return g }
func (g *Registry) Get(path string, fn any, opts ...RouteOption) *Registry            { return g }
func (g *Registry) Post(path string, fn any, opts ...RouteOption) *Registry           { return g }
func (g *Registry) Live(path string, page any, opts ...RouteOption) *Registry         { return g }
func (g *Registry) Controller(prefix string, ctor any, opts ...RouteOption) *Registry { return g }

type App struct{}

func New() *App { return &App{} }

func (a *App) Handle(method, path string, fn any, opts ...RouteOption) *App { return a }
func (a *App) Get(path string, fn any, opts ...RouteOption) *App            { return a }
func (a *App) Post(path string, fn any, opts ...RouteOption) *App           { return a }
func (a *App) Live(path string, page any, opts ...RouteOption) *App         { return a }
func (a *App) Page(path string, component any, attrs ...any) *App           { return a }
func (a *App) Controller(prefix string, ctor any, opts ...RouteOption) *App { return a }
func (a *App) Service(ctor any) *App                                        { return a }

func PageRoute() RouteOption { return opt{} }
