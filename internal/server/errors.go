package server

import (
	"errors"
	"net/http"
)

type badRequestError struct {
	message string
}

func (e *badRequestError) Error() string {
	return e.message
}

type unauthorizedError struct {
	message string
}

func (e *unauthorizedError) Error() string {
	return e.message
}

type notFoundError struct {
	message string
}

func (e *notFoundError) Error() string {
	return e.message
}

func writeProcessorError(w http.ResponseWriter, err error) bool {
	var badRequest *badRequestError
	if errors.As(err, &badRequest) {
		http.Error(w, badRequest.Error(), http.StatusBadRequest)
		return true
	}

	var tooLarge *requestEntityTooLargeError
	if errors.As(err, &tooLarge) {
		http.Error(w, tooLarge.Error(), http.StatusRequestEntityTooLarge)
		return true
	}

	var unauthorized *unauthorizedError
	if errors.As(err, &unauthorized) {
		http.Error(w, unauthorized.Error(), http.StatusUnauthorized)
		return true
	}

	var notFound *notFoundError
	if errors.As(err, &notFound) {
		http.Error(w, notFound.Error(), http.StatusNotFound)
		return true
	}

	return false
}
