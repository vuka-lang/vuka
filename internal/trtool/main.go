package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/vuka-lang/vuka/internal/load"
	"github.com/vuka-lang/vuka/transpile"
)

func main() {
	src, _ := os.ReadFile(os.Args[1])
	dir := filepath.Dir(os.Args[1])
	res, err := transpile.Package([]transpile.File{{Name: filepath.Base(os.Args[1]), Src: src}}, transpile.Options{Importer: load.NewImporter(dir, ""), Dir: dir})
	if err != nil {
		fmt.Println(err)
		return
	}
	os.Stdout.Write(res.Files[0].Src)
}
