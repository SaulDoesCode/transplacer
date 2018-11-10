package mak

import (
	"os"
)

// GET registers a new GET route for the path with the matching h in the router
// with the optional route-level gases.
func (in *Instance) GET(path string, h Handler, wares ...Middleware) {
	in.Router.Register("GET", path, h, wares...)
}

// HEAD registers a new HEAD route for the path with the matching h in the
// router with the optional route-level gases.
func (in *Instance) HEAD(path string, h Handler, wares ...Middleware) {
	in.Router.Register("HEAD", path, h, wares...)
}

// POST registers a new POST route for the path with the matching h in the
// router with the optional route-level gases.
func (in *Instance) POST(path string, h Handler, wares ...Middleware) {
	in.Router.Register("POST", path, h, wares...)
}

// PUT registers a new PUT route for the path with the matching h in the router
// with the optional route-level gases.
func (in *Instance) PUT(path string, h Handler, wares ...Middleware) {
	in.Router.Register("PUT", path, h, wares...)
}

// PATCH registers a new PATCH route for the path with the matching h in the
// router with the optional route-level gases.
func (in *Instance) PATCH(path string, h Handler, wares ...Middleware) {
	in.Router.Register("PATCH", path, h, wares...)
}

// DELETE registers a new DELETE route for the path with the matching h in the
// router with the optional route-level gases.
func (in *Instance) DELETE(path string, h Handler, wares ...Middleware) {
	in.Router.Register("DELETE", path, h, wares...)
}

// CONNECT registers a new CONNECT route for the path with the matching h in the
// router with the optional route-level gases.
func (in *Instance) CONNECT(path string, h Handler, wares ...Middleware) {
	in.Router.Register("CONNECT", path, h, wares...)
}

// OPTIONS registers a new OPTIONS route for the path with the matching h in the
// router with the optional route-level gases.
func (in *Instance) OPTIONS(path string, h Handler, wares ...Middleware) {
	in.Router.Register("OPTIONS", path, h, wares...)
}

// TRACE registers a new TRACE route for the path with the matching h in the
// router with the optional route-level gases.
func (in *Instance) TRACE(path string, h Handler, wares ...Middleware) {
	in.Router.Register("TRACE", path, h, wares...)
}

// STATIC registers a new route with the path prefix to serve the static files
// from the root with the optional route-level gases.
func (in *Instance) STATIC(prefix, root string, wares ...Middleware) {
	h := func(c *Ctx) error {
		err := c.WriteFile(prepPath(root, c.Param("*").String()))
		if os.IsNotExist(err) {
			if in.NotFoundHandler != nil {
				return in.NotFoundHandler(c)
			}
			return ErrNotFound.Envoy(c)
		}

		return err
	}

	in.GET(prefix+"*", h, wares...)
	in.HEAD(prefix+"*", h, wares...)
}

// FILE registers a new route with the path to serve a static file with the
// filename and the optional route-level gases.
func (in *Instance) FILE(path, filename string, wares ...Middleware) {
	h := func(c *Ctx) error {
		err := c.WriteFile(filename)
		if os.IsNotExist(err) {
			if in.NotFoundHandler != nil {
				return in.NotFoundHandler(c)
			}
			return ErrNotFound.Envoy(c)
		}

		return err
	}

	in.GET(path, h, wares...)
	in.HEAD(path, h, wares...)
}
