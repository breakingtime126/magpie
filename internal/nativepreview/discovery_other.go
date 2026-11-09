//go:build !windows

package nativepreview

import "context"

func installedExecutable(context.Context) (string, error) { return "", ErrUnsupported }
