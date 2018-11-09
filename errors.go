package mak

// Err is mak's standard error type
type Err struct {
	Code  int
	Value string
}

func (err *Err) Error() string {
	return err.Value
}

// Send an error response through the context
func (err *Err) Send(c *Ctx) error {
	c.SetStatus(err.Code)
	return nil
}

// Envoy set's the context's status code and just the returns the error
// as representative of what happened. Unlike Send, Envoy leaves writing
// to other handlers down the line.
func (err *Err) Envoy(c *Ctx) error {
	c.Status = err.Code
	return err

}

// MakeErr generates a new Mak error
// totally compatible with conventional go errors
func MakeErr(code int, value string) *Err {
	return &Err{Code: code, Value: value}
}

var (
	// ErrNotFound is the standard 404 error
	ErrNotFound = MakeErr(404, "not found")
	// ErrMethodNotAllowed is the standard 405 error
	ErrMethodNotAllowed = MakeErr(405, "method not allowed")
	// ErrIndeterminateData for when reflection goes wrong or there is malformed data
	ErrIndeterminateData = MakeErr(400, "unparsible or malformed data")
	// ErrUnsupportedMediaType for when a media type cannot be handled
	ErrUnsupportedMediaType = MakeErr(415, "unsupported media type")
	// ErrRequestBodyEmpty request's body content is absent; malformed POST most likely.
	ErrRequestBodyEmpty = MakeErr(400, "request body empty, cannot proceed")
	// ErrBadRange is for when the Requested Range is Not Satisfiable (out of bounds or such)
	ErrBadRange = MakeErr(416, "unsatisfiable range")
	// ErrPreConditionFail a precondition for request completion has not been met
	ErrPreConditionFail = MakeErr(412, "precondition failed")
)
