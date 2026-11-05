// Package devstatic holds, in a build of vuka run -watch, the fixed HTML of
// the module's templates: the watch build compiles one package of its own,
// which loads it here as it initializes, and the packages that render it
// read it by name. A text edit in a template then changes no compiled
// package at all: the watch writes the new texts to a file and starts the
// same binary again, reading them (Load). An older vuka compiles the texts
// into that package instead (Set).
//
// Only a watch build's generated code uses it; nothing else should.
package devstatic

import (
	"encoding/json"
	"fmt"
	"os"
)

// FileEnv names the file a watch build's process reads its texts from:
// vuka run -watch sets it for each process it starts, to the texts of the
// sources that process serves.
const FileEnv = "VUKA_DEV_STATICS_FILE"

// File is what the file FileEnv names holds: the texts, keys and texts in
// turn, and Keys, which tells the binaries whose packages read exactly
// those keys.
type File struct {
	Keys  string   `json:"keys"`
	Texts []string `json:"texts"`
}

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

// Load registers the fixed HTML of the file FileEnv names, whose Keys must
// be keys: the binary's packages read those. The package holding the
// module's texts calls it as it initializes.
func Load(keys string) {
	name := os.Getenv(FileEnv)
	if name == "" {
		panic("devstatic: " + FileEnv + " isn't set: this binary is a vuka run -watch build, which reads its templates' fixed HTML from the file vuka run names; build it with vuka build to run it on its own")
	}
	data, err := os.ReadFile(name)
	if err != nil {
		panic(fmt.Sprintf("devstatic: the templates' fixed HTML: %v", err))
	}
	var f File
	if err := json.Unmarshal(data, &f); err != nil {
		panic(fmt.Sprintf("devstatic: the templates' fixed HTML, %s: %v", name, err))
	}
	if f.Keys != keys {
		panic(fmt.Sprintf("devstatic: %s holds the fixed HTML of another build (keys %s, not %s)", name, f.Keys, keys))
	}
	Set(f.Texts)
}

// S is the fixed HTML named key.
func S(key string) string {
	s, ok := statics[key]
	if !ok {
		panic(fmt.Sprintf("devstatic: no fixed HTML %q: a watch build's package read it before it was registered", key))
	}
	return s
}
