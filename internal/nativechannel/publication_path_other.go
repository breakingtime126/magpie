//go:build !windows

package nativechannel

import "os"

func publicationPathAllowed(_ string, info os.FileInfo) bool {
	return info != nil && info.Mode()&(os.ModeSymlink|os.ModeIrregular) == 0
}
