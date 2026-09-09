package flashsale

import "errors"

var (
	ErrForbiddenTransition       = errors.New("forbidden transition")
	ErrInvalidQuantity           = errors.New("invalid quantity")
	ErrInvalidConfiguration      = errors.New("invalid configuration")
	ErrSaleNotFound              = errors.New("sale not found")
	ErrConflict                  = errors.New("conflict")
	ErrReservationUnavailable    = errors.New("reservation unavailable")
	ErrReservationNotFound       = errors.New("reservation not found")
	ErrSaleItemNotFound          = errors.New("sale item not found")
	ErrOrderNotFound             = errors.New("order not found")
	ErrIdempotencyRecordNotFound = errors.New("idempotency record not found")
	ErrInvalidPagination         = errors.New("invalid pagination")
)
