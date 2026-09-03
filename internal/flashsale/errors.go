package flashsale

import "errors"

var (
	ErrForbiddenTransition    = errors.New("forbidden transition")
	ErrInvalidQuantity        = errors.New("invalid quantity")
	ErrInvalidConfiguration   = errors.New("invalid configuration")
	ErrSaleNotFound           = errors.New("sale not found")
	ErrConflict               = errors.New("conflict")
	ErrReservationUnavailable = errors.New("reservation unavailable")
)
