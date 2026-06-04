// Package resterr defines a typed HTTP error used to communicate status codes through the render layer.
package resterr

import "errors"

type Resterr struct {
	Err        error
	StatusCode int
}

func New(text string, code int) Resterr {
	return Resterr{Err: errors.New(text), StatusCode: code}
}

func FromErr(err error, code int) Resterr {
	return Resterr{Err: err, StatusCode: code}
}
