//go:build !unix

package buildlock

func lockShared(string) (func(), bool) { return func() {}, false }

// lockTryExclusive never succeeds: without file locks nothing is pruned.
func lockTryExclusive(string) (func(), bool) { return func() {}, false }
