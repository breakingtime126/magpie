//go:build !windows

package nativeconnect

type unsupportedSelection struct{}

func SystemSelection() Store { return unsupportedSelection{} }

func (unsupportedSelection) Read() (Selection, error) {
	return Selection{}, ErrUnavailable
}

func (unsupportedSelection) Replace(Selection, Selection) error {
	return ErrUnavailable
}
