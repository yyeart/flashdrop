package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/yyeart/flashdrop/internal/identity"
)

type identityService interface {
	Register(
		ctx context.Context,
		input identity.RegisterInput,
	) (identity.User, error)

	Login(
		ctx context.Context,
		input identity.LoginInput,
	) (identity.LoginResult, error)
}

type AuthHandler struct {
	identity identityService
	logger   *slog.Logger
}

func NewAuthHandler(
	service identityService,
	logger *slog.Logger,
) (*AuthHandler, error) {
	if service == nil {
		return nil, ErrNilIdentityService
	}

	if logger == nil {
		return nil, ErrNilLogger
	}

	return &AuthHandler{
		identity: service,
		logger:   logger,
	}, nil
}

func (h *AuthHandler) Register(
	w http.ResponseWriter, r *http.Request,
) {
	if r.Context().Err() != nil {
		return
	}

	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeErrorAndLog(
			h.logger,
			w,
			r,
			http.StatusMethodNotAllowed,
			codeMethodNotAllowed,
			"method not allowed",
		)

		return
	}

	var request registerRequest
	if err := decodeJSON(w, r, &request); err != nil {
		if r.Context().Err() != nil {
			return
		}

		writeErrorAndLog(
			h.logger,
			w,
			r,
			http.StatusBadRequest,
			codeBadRequest,
			"invalid request body",
		)

		return
	}

	user, err := h.identity.Register(r.Context(), identity.RegisterInput{
		Email:    request.Email,
		Password: request.Password,
	})
	if err != nil {
		if r.Context().Err() != nil {
			return
		}

		h.handleRegisterError(w, r, err)
		return
	}

	response := registerResponse{
		ID:        user.ID(),
		Email:     user.Email(),
		Role:      user.Role(),
		CreatedAt: user.CreatedAt(),
	}

	if r.Context().Err() != nil {
		return
	}

	if err := writeJson(w, http.StatusCreated, response); err != nil {
		logHandlerFailure(h.logger, w, r, "register_response_write_failed")
	}
}

func (h *AuthHandler) handleRegisterError(
	w http.ResponseWriter,
	r *http.Request,
	err error,
) {
	switch {
	case errors.Is(err, identity.ErrInvalidEmail):
		writeErrorAndLog(
			h.logger,
			w,
			r,
			http.StatusBadRequest,
			codeBadRequest,
			"invalid email",
		)

	case errors.Is(err, identity.ErrInvalidPasswordLength):
		writeErrorAndLog(
			h.logger,
			w,
			r,
			http.StatusBadRequest,
			codeBadRequest,
			"password must be between 8 and 128 bytes",
		)

	case errors.Is(err, identity.ErrEmailAlreadyExists):
		writeErrorAndLog(
			h.logger,
			w,
			r,
			http.StatusConflict,
			codeConflict,
			"email already exists",
		)

	default:
		logHandlerFailure(h.logger, w, r, "identity_register_failed")

		writeErrorAndLog(
			h.logger,
			w,
			r,
			http.StatusInternalServerError,
			codeInternalError,
			"internal server error",
		)
	}
}

func (h *AuthHandler) Login(
	w http.ResponseWriter,
	r *http.Request,
) {
	if r.Context().Err() != nil {
		return
	}

	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeErrorAndLog(
			h.logger,
			w,
			r,
			http.StatusMethodNotAllowed,
			codeMethodNotAllowed,
			"method not allowed",
		)

		return
	}

	var request loginRequest
	if err := decodeJSON(w, r, &request); err != nil {
		if r.Context().Err() != nil {
			return
		}

		writeErrorAndLog(
			h.logger,
			w,
			r,
			http.StatusBadRequest,
			codeBadRequest,
			"invalid request body",
		)

		return
	}

	result, err := h.identity.Login(r.Context(), identity.LoginInput{
		Email:    request.Email,
		Password: request.Password,
	})
	if err != nil {
		if r.Context().Err() != nil {
			return
		}

		h.handleLoginError(w, r, err)
		return
	}

	response := loginResponse{
		AccessToken: result.AccessToken,
		TokenType:   "Bearer",
		ExpiresIn:   int64(result.ExpiresIn / time.Second),
	}

	if r.Context().Err() != nil {
		return
	}

	if err := writeJson(w, http.StatusOK, response); err != nil {
		logHandlerFailure(h.logger, w, r, "login_response_write_failed")
	}
}

func (h *AuthHandler) handleLoginError(
	w http.ResponseWriter,
	r *http.Request,
	err error,
) {
	switch {
	case errors.Is(err, identity.ErrInvalidCredentials):
		writeErrorAndLog(
			h.logger,
			w,
			r,
			http.StatusUnauthorized,
			codeUnauthorized,
			"invalid credentials",
		)

	case errors.Is(err, identity.ErrInvalidPasswordLength):
		writeErrorAndLog(
			h.logger,
			w,
			r,
			http.StatusBadRequest,
			codeBadRequest,
			"password must be between 8 and 128 bytes",
		)

	default:
		logHandlerFailure(h.logger, w, r, "identity_login_failed")

		writeErrorAndLog(
			h.logger,
			w,
			r,
			http.StatusInternalServerError,
			codeInternalError,
			"internal server error",
		)
	}
}

func logHandlerFailure(
	logger *slog.Logger,
	w http.ResponseWriter,
	r *http.Request,
	reason string,
) {
	requestID := requestIDForResponse(r.Context(), w)

	logger.ErrorContext(
		r.Context(),
		"HTTP handler failed",
		slog.String("reason", reason),
		slog.String("request_id", requestID),
		slog.String("method", r.Method),
		slog.String("path", r.URL.Path),
	)
}
