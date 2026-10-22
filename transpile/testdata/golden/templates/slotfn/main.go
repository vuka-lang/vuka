package main

import (
	"context"
	"fmt"

	"github.com/vuka-lang/ui/v2"
)

type PanelProps struct {
	Title    string
	Header   ui.Node
	Children ui.Node
}

func main() {
	out, err := ui.String(context.Background(), Page{Items: []string{"a", "b"}})
	fmt.Println(out, err)
}

func Panel(p PanelProps) ui.Node {
	return ui.HTML(`<section><h2>{p.Title}</h2>{p.Header}{p.Children}</section>`, p.Title, p.Header, p.Children)
}

// Frame takes its slot as a parameter.
func Frame(title string, header, children ui.Node) ui.Node {
	return ui.HTML(`<div title={title}>{header}{children}</div>`, title, header, children)
}
