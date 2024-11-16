package middleware

import "net/http"

// Chain applies multiple middleware to a handler in the order they are provided
func Chain(h http.Handler, middleware ...func(http.Handler) http.Handler) http.Handler {
	for _, m := range middleware {
		h = m(h)
	}
	return h
}
