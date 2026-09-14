package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
)

const maxJSONBodyBytes int64 = 1 << 20

func decodeJSON(w http.ResponseWriter, r *http.Request, dest any) error {
	if err := validateJSONContentType(r.Header); err != nil {
		return fmt.Errorf("validate JSON Content-Type: %w", err)
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)

	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(dest); err != nil {
		if errors.Is(err, io.EOF) {
			return ErrEmptyBody
		}

		return classifyJSONDecodeError(err)
	}

	var extra json.RawMessage
	if err := decoder.Decode(&extra); err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}

		return classifyJSONDecodeError(err)
	}

	return ErrMultipleJSONValues
}

func writeJson(w http.ResponseWriter, status int, value any) error {
	body, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("marshal response body: %w", err)
	}

	body = append(body, '\n')

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	if _, err := w.Write(body); err != nil {
		return fmt.Errorf("write response body: %w", err)
	}

	return nil
}

func validateJSONContentType(header http.Header) error {
	values := header.Values("Content-Type")
	if len(values) != 1 {
		return ErrUnsupportedMediaType
	}

	mediaType, _, err := mime.ParseMediaType(values[0])
	if err != nil {
		return fmt.Errorf(
			"parse Content-Type: %w: %w",
			err,
			ErrUnsupportedMediaType,
		)
	}

	if !strings.EqualFold(mediaType, "application/json") {
		return fmt.Errorf(
			"Content-Type: %q: %w",
			mediaType,
			ErrUnsupportedMediaType,
		)
	}

	return nil
}

func classifyJSONDecodeError(err error) error {
	var maxBytesErr *http.MaxBytesError
	if errors.As(err, &maxBytesErr) {
		return fmt.Errorf(
			"body exceeds %d bytes: %w",
			maxBytesErr.Limit,
			ErrBodyTooLarge,
		)
	}

	var invalidTargetErr *json.InvalidUnmarshalError
	if errors.As(err, &invalidTargetErr) {
		return fmt.Errorf("invalid JSON decode target: %w", err)
	}

	if strings.HasPrefix(err.Error(), "json: unknown field ") {
		field := strings.TrimPrefix(err.Error(), "json: unknown field ")

		return fmt.Errorf(
			"field %s: %w",
			field, ErrUnknownJSONField,
		)
	}

	var syntaxErr *json.SyntaxError
	if errors.As(err, &syntaxErr) {
		return fmt.Errorf(
			"JSON syntax error at byte %d: %w",
			syntaxErr.Offset,
			ErrMalformedJSON,
		)
	}

	if errors.Is(err, io.ErrUnexpectedEOF) {
		return ErrMalformedJSON
	}

	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &typeErr) {
		if typeErr.Field == "" {
			return fmt.Errorf(
				"value at byte %d: %w",
				typeErr.Offset,
				ErrInvalidJSONType,
			)
		}

		return fmt.Errorf(
			"field %q at byte %d: %w",
			typeErr.Field,
			typeErr.Offset,
			ErrInvalidJSONType,
		)
	}

	return fmt.Errorf("read request body: %w", err)
}
