package api

import "errors"

var (
	ErrMaxWorkers         = errors.New("maximum number of workers")
	ErrBeadAlreadyClaimed = errors.New("bead already claimed")
)
