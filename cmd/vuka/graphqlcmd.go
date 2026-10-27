package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const graphqlUsage = `usage: vuka graphql schema [-check file] [-o file] [package]

Schema prints the GraphQL schema the app serves (web.GraphQL), as SDL: it
builds the app's main package (the current directory's by default) and
runs it until its schema is built, its operations' names decided by the
app's options and attributes. Nothing is served, and the app's services
aren't started; code main runs before web.New still runs.

-o writes the SDL to a file. -check compares the schema with the SDL in a
file, a committed schema.graphql, printing each change, and fails when one
breaks clients: a type, field, argument or enum value removed or renamed,
a type changed, an input made non-null, an output made nullable.
`

// The environment the app runs with for vuka graphql schema (package
// graphql's SchemaEnv and CheckEnv).
const (
	graphqlSchemaEnv = "VUKA_GRAPHQL_SCHEMA"
	graphqlCheckEnv  = "VUKA_GRAPHQL_CHECK"
)

func graphqlCmd(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 || args[0] != "schema" {
		fmt.Fprint(stderr, graphqlUsage)
		return exitStatus(2)
	}
	fs := flag.NewFlagSet("graphql schema", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprint(fs.Output(), graphqlUsage) }
	check := fs.String("check", "", "compare the schema with the SDL in `file`; fail on breaking changes")
	out := fs.String("o", "", "write the SDL to `file`")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	pkg := "."
	switch fs.NArg() {
	case 0:
	case 1:
		pkg = fs.Arg(0)
	default:
		fs.Usage()
		return exitStatus(2)
	}
	env := map[string]string{"VUKA_TAILWIND": "off", "VUKA_FRONTEND": "off"}
	switch {
	case *check != "":
		abs, err := filepath.Abs(*check)
		if err != nil {
			return err
		}
		if _, err := os.Stat(abs); err != nil {
			return err
		}
		env[graphqlCheckEnv] = abs
	case *out != "":
		abs, err := filepath.Abs(*out)
		if err != nil {
			return err
		}
		env[graphqlSchemaEnv] = abs
	default:
		env[graphqlSchemaEnv] = "-"
	}
	for k, v := range env {
		os.Setenv(k, v)
	}
	return runGo("run", []string{pkg}, stdout, stderr)
}
