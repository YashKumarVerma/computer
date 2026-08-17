package sdk

import "errors"

var (
	ErrNotFound         = errors.New("storage: key not found")
	ErrWrongType        = errors.New("storage: value is not of the requested type")
	ErrUnknownDimension = errors.New("operation: dimension not declared at registration")
)
