//go:build !linux

package main

import "errors"

func newNsjail(config) (Sandbox, error) {
	return nil, errors.New("the nsjail sandbox runs on Linux: set RUNNER_SANDBOX=local (and RUNNER_UNSAFE_LOCAL=1) to develop elsewhere")
}
