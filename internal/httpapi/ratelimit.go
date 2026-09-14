package httpapi

import (
	"context"
	"time"
)

type RateLimiter interface {
	Allow(
		ctx context.Context,
		method string,
		path string,
		clientIP string,
	) (
		allowed bool,
		retryAfter time.Duration,
		err error,
	)
}

type AllowAllRateLimiter struct{}

func (AllowAllRateLimiter) Allow(
	ctx context.Context,
	method string,
	path string,
	clientIP string,
) (allowed bool, retryAfter time.Duration, err error) {
	return true, 0, nil
}
