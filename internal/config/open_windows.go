//go:build windows

package config

import "os"

// openSecure opens the config file for writing.
//
// Windows has no O_NOFOLLOW. Reparse points are the rough equivalent of a
// symlink, and creating one normally requires elevation or developer mode, so
// the exposure is much narrower than on Unix. The explicit Chmod the caller
// applies still matters.
func openSecure(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
}
