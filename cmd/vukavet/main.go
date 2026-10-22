// Command vukavet is go vet with vuka's checks: the standard vet analyzers
// and those of package check, which report what a vuka app's boot, its
// render or vuka vet would, with the same codes (vuka explain E0401).
//
//	go get -tool github.com/vuka-lang/vuka/cmd/vukavet/v2
//	go vet -vettool=$(go tool -n vukavet) ./...
//
// It also runs on its own: go tool vukavet ./...
package main

import (
	"golang.org/x/tools/go/analysis/multichecker"
	"golang.org/x/tools/go/analysis/suite/vet"

	"github.com/vuka-lang/vuka/cmd/vukavet/v2/check"
)

func main() { multichecker.Main(append(vet.Suite, check.Analyzers...)...) }
