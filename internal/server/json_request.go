package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

// JSON request helpers centralize bounded decoding for server write handlers.
// That keeps body-size limits and bad-request mapping consistent across auth,
// check management, and probe ingestion endpoints.
const (
	maxJSONRequestBodyBytes      int64 = 1 << 20
	maxProbeJSONRequestBodyBytes int64 = 64 << 10
)

type requestEntityTooLargeError struct {
	message string
}

func (e *requestEntityTooLargeError) Error() string {
	return e.message
}

func decodeJSONBody(w http.ResponseWriter, r *http.Request, dst any, maxBytes int64, allowEmpty bool) error {
	if maxBytes <= 0 {
		maxBytes = maxJSONRequestBodyBytes
	}

	if r.Body == nil {
		if allowEmpty {
			return nil
		}
		return &badRequestError{message: "bad request"}
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(dst); err != nil {
		if allowEmpty && errors.Is(err, io.EOF) {
			return nil
		}
		return mapJSONDecodeError(err)
	}

	if err := dec.Decode(new(struct{})); err != io.EOF {
		return mapJSONDecodeError(err)
	}

	return nil
}

func mapJSONDecodeError(err error) error {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		return &requestEntityTooLargeError{message: "request body too large"}
	}
	return &badRequestError{message: "bad request"}
}
