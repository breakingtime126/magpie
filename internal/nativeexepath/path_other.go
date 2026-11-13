//go:build !windows

// Package nativeexepath resolves executable identity without requiring Windows
// parent directory enumeration. It never grants execution or changes permissions.
package nativeexepath

import "path/filepath"

func Resolve(path string) (string, error) { return filepath.EvalSymlinks(path) }
