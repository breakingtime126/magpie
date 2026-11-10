//go:build !windows

package nativeendpoint

import "errors"

// Listen never substitutes an ordinary, nonexclusive socket on another OS.
func Listen() (*Endpoint, error) { return nil, errors.ErrUnsupported }
