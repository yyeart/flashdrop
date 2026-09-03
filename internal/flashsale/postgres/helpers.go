package flashsale_postgres

import (
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/yyeart/flashdrop/internal/flashsale"
)

func mapDatabaseError(operation string, err error) error {
	var pgErr *pgconn.PgError

	if errors.As(err, &pgErr) &&
		pgErr.Code == postgresUniqueViolation {
		return fmt.Errorf(
			"%s: %w: %w",
			operation,
			flashsale.ErrConflict,
			err,
		)
	}

	return fmt.Errorf("%s: %w", operation, err)
}
