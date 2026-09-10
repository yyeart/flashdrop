package identity_postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/yyeart/flashdrop/internal/identity"
)

func (s *Store) CreateUser(
	ctx context.Context,
	user identity.User,
	passwordHash identity.PasswordHash,
) error {
	hash, err := passwordHash.Encoded()
	if err != nil {
		return err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx) //nolint:errcheck // rollback is best effort after the operation result is known
	}()

	insertUserQuery := `
		INSERT INTO flashdrop.users (
			id, role, created_at
		)
		VALUES ($1, $2, $3);
	`

	if _, err := tx.Exec(
		ctx, insertUserQuery,
		user.ID(), identity.RoleUser, user.CreatedAt(),
	); err != nil {
		return fmt.Errorf("insert user: %w", err)
	}

	insertUserCredentialsQuery := `
		INSERT INTO flashdrop.user_credentials (
			user_id, email, password_hash
		)
		VALUES ($1, $2, $3);
	`

	if _, err := tx.Exec(
		ctx, insertUserCredentialsQuery,
		user.ID(), user.Email(), hash,
	); err != nil {
		return mapRegistrationError(err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("transaction commit: %w", err)
	}

	return nil
}

func mapRegistrationError(err error) error {
	var pgErr *pgconn.PgError

	if errors.As(err, &pgErr) &&
		pgErr.Code == "23505" &&
		pgErr.ConstraintName == "uq_user_credentials_email" {
		return identity.ErrEmailAlreadyExists
	}

	return fmt.Errorf("persist credentials: %w", err)
}
