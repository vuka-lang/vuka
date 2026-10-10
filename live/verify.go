package live

import (
	"os"
	"testing"
)

// Verify makes new sessions render the components Assign tracking would
// skip, and fail the event (or message) when the render differs from the one
// kept: a state field changed without Assign.Set, or Render read something
// else. It is on in tests and with VUKA_LIVE_VERIFY=1, off with
// VUKA_LIVE_VERIFY=0. Verifying sessions also check that statics shared by a
// fingerprint are the same.
var Verify = verifyDefault()

func verifyDefault() bool {
	switch os.Getenv("VUKA_LIVE_VERIFY") {
	case "1", "true":
		return true
	case "0", "false":
		return false
	}
	return testing.Testing()
}
