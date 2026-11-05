// Package devstatic holds, in a build of vuka run -watch, the fixed HTML of
// the module's templates: the watch build compiles it into one package of
// its own, which registers it here as it initializes, and the packages that
// render it read it by name. A text edit in a template then changes that
// package alone, not the compiled package of the template, which every
// package importing it would be compiled again for.
//
// Only a watch build's generated code uses it; nothing else should.
package devstatic

import "fmt"

var statics map[string]string

// Set registers the module's fixed HTML: keys and texts in turn. The
// package holding it calls it as it initializes, before the packages it is
// read by.
func Set(pairs []string) {
	statics = make(map[string]string, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		statics[pairs[i]] = pairs[i+1]
	}
}

// S is the fixed HTML named key.
func S(key string) string {
	s, ok := statics[key]
	if !ok {
		panic(fmt.Sprintf("devstatic: no fixed HTML %q: a watch build's package read it before it was registered", key))
	}
	return s
}
