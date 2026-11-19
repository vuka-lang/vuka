//go:build !unix

package main

func lockFileShared(string) (func(), bool) { return func() {}, false }

// lockFileTryExclusive never succeeds: without file locks nothing is pruned.
func lockFileTryExclusive(string) (func(), bool) { return func() {}, false }
