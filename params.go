package mak

import (
	"errors"
	"fmt"
	"mime/multipart"
	"strconv"
)

// RequestParam is an HTTP request param.
type RequestParam struct {
	Name   string
	Values []*RequestParamValue
}

// Value returns the first value of the rp. It returns nil if the rp is nil or
// there are no values.
func (rp *RequestParam) Value() *RequestParamValue {
	if rp == nil || len(rp.Values) == 0 {
		return nil
	}

	return rp.Values[0]
}

// RequestParamValue is an HTTP request param value.
type RequestParamValue struct {
	ctx  *Ctx
	i    interface{}
	b    *bool
	i64  *int64
	ui64 *uint64
	f64  *float64
	s    *string
	f    *RequestParamFileValue
}

// Bool returns a `bool` from the rpv's underlying value.
func (rpv *RequestParamValue) Bool() (bool, error) {
	if rpv.b == nil {
		b, err := strconv.ParseBool(rpv.String())
		if err != nil {
			return false, err
		}

		rpv.b = &b
	}

	return *rpv.b, nil
}

// Int returns an `int` from the rpv's underlying value.
func (rpv *RequestParamValue) Int() (int, error) {
	if rpv.i64 == nil {
		i64, err := strconv.ParseInt(rpv.String(), 10, 0)
		if err != nil {
			return 0, err
		}

		rpv.i64 = &i64
	}

	return int(*rpv.i64), nil
}

// Int8 returns an `int8` from the rpv's underlying value.
func (rpv *RequestParamValue) Int8() (int8, error) {
	if rpv.i64 == nil {
		i64, err := strconv.ParseInt(rpv.String(), 10, 8)
		if err != nil {
			return 0, err
		}

		rpv.i64 = &i64
	}

	return int8(*rpv.i64), nil
}

// Int16 returns an `int16` from the rpv's underlying value.
func (rpv *RequestParamValue) Int16() (int16, error) {
	if rpv.i64 == nil {
		i64, err := strconv.ParseInt(rpv.String(), 10, 16)
		if err != nil {
			return 0, err
		}

		rpv.i64 = &i64
	}

	return int16(*rpv.i64), nil
}

// Int32 returns an `int32` from the rpv's underlying value.
func (rpv *RequestParamValue) Int32() (int32, error) {
	if rpv.i64 == nil {
		i64, err := strconv.ParseInt(rpv.String(), 10, 32)
		if err != nil {
			return 0, err
		}

		rpv.i64 = &i64
	}

	return int32(*rpv.i64), nil
}

// Int64 returns an `int64` from the rpv's underlying value.
func (rpv *RequestParamValue) Int64() (int64, error) {
	if rpv.i64 == nil {
		i64, err := strconv.ParseInt(rpv.String(), 10, 64)
		if err != nil {
			return 0, err
		}

		rpv.i64 = &i64
	}

	return *rpv.i64, nil
}

// Uint returns an `uint` from the rpv's underlying value.
func (rpv *RequestParamValue) Uint() (uint, error) {
	if rpv.ui64 == nil {
		ui64, err := strconv.ParseUint(rpv.String(), 10, 0)
		if err != nil {
			return 0, err
		}

		rpv.ui64 = &ui64
	}

	return uint(*rpv.ui64), nil
}

// Uint8 returns an `uint8` from the rpv's underlying value.
func (rpv *RequestParamValue) Uint8() (uint8, error) {
	if rpv.ui64 == nil {
		ui64, err := strconv.ParseUint(rpv.String(), 10, 8)
		if err != nil {
			return 0, err
		}

		rpv.ui64 = &ui64
	}

	return uint8(*rpv.ui64), nil
}

// Uint16 returns an `uint16` from the rpv's underlying value.
func (rpv *RequestParamValue) Uint16() (uint16, error) {
	if rpv.ui64 == nil {
		ui64, err := strconv.ParseUint(rpv.String(), 10, 16)
		if err != nil {
			return 0, err
		}

		rpv.ui64 = &ui64
	}

	return uint16(*rpv.ui64), nil
}

// Uint32 returns an `uint32` from the rpv's underlying value.
func (rpv *RequestParamValue) Uint32() (uint32, error) {
	if rpv.ui64 == nil {
		ui64, err := strconv.ParseUint(rpv.String(), 10, 32)
		if err != nil {
			return 0, err
		}

		rpv.ui64 = &ui64
	}

	return uint32(*rpv.ui64), nil
}

// Uint64 returns an `uint64` from the rpv's underlying value.
func (rpv *RequestParamValue) Uint64() (uint64, error) {
	if rpv.ui64 == nil {
		ui64, err := strconv.ParseUint(rpv.String(), 10, 64)
		if err != nil {
			return 0, err
		}

		rpv.ui64 = &ui64
	}

	return *rpv.ui64, nil
}

// Float32 returns a `float32` from the rpv's underlying value.
func (rpv *RequestParamValue) Float32() (float32, error) {
	if rpv.f64 == nil {
		f64, err := strconv.ParseFloat(rpv.String(), 32)
		if err != nil {
			return 0, err
		}

		rpv.f64 = &f64
	}

	return float32(*rpv.f64), nil
}

// Float64 returns a `float64` from the rpv's underlying value.
func (rpv *RequestParamValue) Float64() (float64, error) {
	if rpv.f64 == nil {
		f64, err := strconv.ParseFloat(rpv.String(), 64)
		if err != nil {
			return 0, err
		}

		rpv.f64 = &f64
	}

	return *rpv.f64, nil
}

// String returns a `string` from the rpv's underlying value.
func (rpv *RequestParamValue) String() string {
	if rpv.s == nil {
		if s, ok := rpv.i.(string); ok {
			rpv.s = &s
		} else {
			s := fmt.Sprintf("%v", rpv.i)
			rpv.s = &s
		}
	}

	return *rpv.s
}

// File returns a `RequestParamFileValue` from the rpv's underlying value.
func (rpv *RequestParamValue) File() (*RequestParamFileValue, error) {
	if rpv.f == nil {
		fh, ok := rpv.i.(*multipart.FileHeader)
		if !ok {
			return nil, errors.New("not a request param file value")
		}

		rpv.f = &RequestParamFileValue{
			Filename:      fh.Filename,
			ContentLength: fh.Size,

			fh: fh,
		}

		for name := range fh.Header {
			rpv.ctx.SetHeader(name, fh.Header.Get(name))
		}
	}

	return rpv.f, nil
}

// RequestParamFileValue is an HTTP request param file value.
type RequestParamFileValue struct {
	Filename      string
	ContentLength int64

	fh *multipart.FileHeader
	f  multipart.File
}

// Read implements the `io.Reader`.
func (v *RequestParamFileValue) Read(b []byte) (int, error) {
	if v.f == nil {
		var err error
		if v.f, err = v.fh.Open(); err != nil {
			return 0, err
		}
	}

	return v.f.Read(b)
}

// Seek implements the `io.Seeker`.
func (v *RequestParamFileValue) Seek(offset int64, whence int) (int64, error) {
	if v.f == nil {
		var err error
		if v.f, err = v.fh.Open(); err != nil {
			return 0, err
		}
	}

	return v.f.Seek(offset, whence)
}

// Param returns the matched `RequestParam` for the name. It returns nil if not
// found.
func (c *Ctx) Param(name string) *RequestParam {
	c.parseParamsOnce.Do(c.parseParams)

	for _, p := range c.params {
		if p.Name == name {
			return p
		}
	}

	return nil
}

// Params returns all the `RequestParam` in the c.
func (c *Ctx) Params() []*RequestParam {
	c.parseParamsOnce.Do(c.parseParams)
	return c.params
}

// parseParams parses the params sent with the r into the `c.params`.
func (c *Ctx) parseParams() {
	if c.R.Form == nil || c.R.MultipartForm == nil {
		c.R.ParseMultipartForm(32 << 20)
	}

FormLoop:
	for n, vs := range c.R.Form {
		pvs := make([]*RequestParamValue, 0, len(vs))
		for _, v := range vs {
			pvs = append(pvs, &RequestParamValue{
				ctx: c,
				i:   v,
			})
		}

		for _, p := range c.params {
			if p.Name == n {
				p.Values = append(p.Values, pvs...)
				continue FormLoop
			}
		}

		c.params = append(c.params, &RequestParam{
			Name:   n,
			Values: pvs,
		})
	}

	if c.R.MultipartForm != nil {
	MultipartFormValueLoop:
		for n, vs := range c.R.MultipartForm.Value {
			pvs := make([]*RequestParamValue, 0, len(vs))
			for _, v := range vs {
				pvs = append(pvs, &RequestParamValue{
					ctx: c,
					i:   v,
				})
			}

			for _, p := range c.params {
				if p.Name == n {
					p.Values = append(p.Values, pvs...)
					continue MultipartFormValueLoop
				}
			}

			c.params = append(c.params, &RequestParam{
				Name:   n,
				Values: pvs,
			})
		}

	MultipartFormFileLoop:
		for n, vs := range c.R.MultipartForm.File {
			pvs := make([]*RequestParamValue, 0, len(vs))
			for _, v := range vs {
				pvs = append(pvs, &RequestParamValue{
					ctx: c,
					i:   v,
				})
			}

			for _, p := range c.params {
				if p.Name == n {
					p.Values = append(p.Values, pvs...)
					continue MultipartFormFileLoop
				}
			}

			c.params = append(c.params, &RequestParam{Name: n, Values: pvs})
		}
	}
}
