package flashsale

import "errors"

var (
	ErrForbiddenTransition  = errors.New("forbidden transition")
	ErrInvalidQuantity      = errors.New("invalid quantity")
	ErrInvalidConfiguration = errors.New("invalid configuration")
)
