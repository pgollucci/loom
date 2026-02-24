package beads

import "errors"

// Sentinel errors for bead operations. Use errors.Is() to check these
// rather than inspecting error message strings.
var (
	ErrBeadNotFound       = errors.New("bead not found")
	ErrBeadAlreadyClaimed = errors.New("bead already claimed")
)
