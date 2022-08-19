package ws

import "errors"

var (
	ErrMissingUpgrade         = errors.New("MISSING UPGRADE")
	ErrInvalidUpgrade         = errors.New("INVALID UPGRADE")
	ErrInvalidWebsocketKey    = errors.New("INVALID WEBSOCKET KEY")
	ErrInvalidRSV             = errors.New("INVALID RSV")
	ErrInvalidOpcode          = errors.New("INVALID OPCODE")
	ErrReservedOpcode         = errors.New("RESERVED OPCODE")
	ErrInvalidContinuation    = errors.New("INVALID CONTINUATION FRAME")
	ErrUnmaskedframe          = errors.New("UNMASKED FRAME")
	ErrFragmentedControlFrame = errors.New("FRAGMENTED CONTROL FRAME")
	ErrInvalidClosePayload    = errors.New("INVALID CLOSE PAYLOAD")
	ErrInvalidUTF8            = errors.New("INVALID UTF8")
)
