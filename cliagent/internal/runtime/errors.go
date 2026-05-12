package runtime

import "errors"

var (
	ErrSessionBusy         = errors.New("runtime: session busy")
	ErrNoActiveStream      = errors.New("runtime: no active stream")
	ErrStreamClosed        = errors.New("runtime: stream closed")
	ErrSteeringUnavailable = errors.New("runtime: steering unavailable")
	ErrFollowUpUnavailable = errors.New("runtime: follow-up unavailable")
	ErrIncompatibleOption  = errors.New("runtime: incompatible option")
	ErrPermissionResponse  = errors.New("runtime: permission response unavailable")
)
