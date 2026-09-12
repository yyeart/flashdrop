package identity_postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/yyeart/flashdrop/internal/identity"
)

func (s *Store) FindLoginCreds(
	ctx context.Context,
	email string,
) (identity.LoginCredentials, bool, error) {
	query := `
		SELECT
			u.id, u.role, c.password_hash
		FROM flashdrop.users AS u
		JOIN flashdrop.user_credentials AS c
			ON c.user_id = u.id
		WHERE c.email = $1;
	`

	var credentials identity.LoginCredentials
	var encodedHash string

	if err := s.pool.QueryRow(ctx, query, email).Scan(
		&credentials.UserID, &credentials.Role, &encodedHash,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return identity.LoginCredentials{}, false, nil
		}

		return identity.LoginCredentials{}, false, fmt.Errorf("scan error: %w", err)
	}

	hash, err := identity.ParsePasswordHash(encodedHash)
	if err != nil {
		return identity.LoginCredentials{}, false, fmt.Errorf(
			"parse stored password hash: %w", err,
		)
	}

	credentials.PasswordHash = hash

	return credentials, true, nil
}
