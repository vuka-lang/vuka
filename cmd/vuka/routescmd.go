package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/vuka-lang/vuka/v2/diag"
	"github.com/vuka-lang/vuka/v2/toolchain/load"
)

const routesUsage = `usage: vuka routes [-json]

Routes prints the module's folder routes, the pages under pages/: each
route's method, path, page or endpoint type, where it is declared and the
middleware, guards and layouts over it, outermost first. -json prints them
as a JSON array.
`

// routesCmd is vuka routes.
func routesCmd(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("routes", flag.ContinueOnError)
	fs.Usage = func() { fmt.Fprint(fs.Output(), routesUsage) }
	asJSON := fs.Bool("json", false, "print the routes as JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	root, modPath, err := load.ModuleRoot(".")
	if err != nil {
		return err
	}
	routes, res, err := routeTable(root, modPath)
	if err != nil {
		return err
	}
	for _, r := range res.routes {
		r.File = relPath(root, r.File)
	}
	res.diags = append(res.diags, routesUnserved(root, modPath, routes)...)
	res.routes = sortedRoutes(res.routes)
	if *asJSON {
		list := res.routes
		if list == nil {
			list = []*folderRoute{}
		}
		b, err := json.MarshalIndent(list, "", "\t")
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "%s\n", b)
	} else if len(routes) == 0 {
		fmt.Fprintln(stdout, "no pages/ directory: the module has no folder routes")
	} else {
		writeRoutes(stdout, res.routes)
	}
	if len(res.diags) > 0 {
		writeDiags(stderr, res.diags, colorFor(stderr))
	}
	if res.err() != nil {
		return exitStatus(1)
	}
	return nil
}

// routeTable transpiles the module and builds its folder routes' table.
func routeTable(root, modPath string) (routeTrees, *routesResult, error) {
	routes := scanRoutes(root, modPath, nil)
	if len(routes) == 0 {
		return nil, &routesResult{}, nil
	}
	pkgs, err := load.Discover(root, modPath, root, true, nil)
	if err != nil {
		return nil, nil, err
	}
	pa, err := findAssets(root, modPath)
	if err != nil {
		return nil, nil, err
	}
	tmp, err := os.MkdirTemp("", "vuka-")
	if err != nil {
		return nil, nil, err
	}
	defer os.RemoveAll(tmp)
	if pkgs, err = routes.stub(pkgs); err != nil {
		ds, _ := diag.From(err)
		return routes, &routesResult{diags: ds}, nil
	}
	gens, overlay, err := cachedTranspile(root)(pkgs, tmp, loadOptions(pa))
	if err != nil {
		return nil, nil, err
	}
	_, _, res, err := routes.finish(gens, overlay, tmp)
	return routes, res, err
}

// writeRoutes prints the table.
func writeRoutes(w io.Writer, routes []*folderRoute) {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "METHOD\tPATH\tPAGE\tFILE\tMIDDLEWARE\tGUARDS\tLAYOUTS")
	for _, r := range routes {
		page := r.Page
		if r.Kind == "live" || r.Kind == "template" || r.Kind == "endpoint" {
			page += " (" + r.Kind + ")"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", r.Method, r.Path, page, r.File+":"+strconv.Itoa(r.Line),
			strings.Join(r.Middleware, ", "), strings.Join(r.Guards, ", "), strings.Join(r.Layouts, ", "))
	}
	tw.Flush()
}
