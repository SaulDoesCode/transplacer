package mak

import (
	"io"
	"net/http"
	"net/url"
	"sync"
)

// Ctx handler context
type Ctx struct {
	R             *http.Request
	W             http.ResponseWriter
	Body          io.Writer
	ContentLength int64
	Written       bool
	Status        int
	Path          string

	instance               *Instance
	clientAddress          string
	parseClientAddressOnce *sync.Once
	params                 []*RequestParam
	parseParamsOnce        *sync.Once
	query                  url.Values
}

// Cookie is alias for http.Cookie
type Cookie = http.Cookie

// Handler defines a function to serve requests.
type Handler func(c *Ctx) error

// Middleware defines a function to process gases.
type Middleware func(Handler) Handler

// WrapHTTPMiddleware is a convenience method allowing the use of conventional
// `http.Handler` middleware by wrapping it and internally converting the
// middleware into a `Gas`.
func WrapHTTPMiddleware(m func(http.Handler) http.Handler) Middleware {
	return func(next Handler) Handler {
		return func(c *Ctx) error {
			var err error
			m(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
				c.R = r
				c.W = rw
				err = next(c)
			})).ServeHTTP(c.W, c.R)
			return err
		}
	}
}
