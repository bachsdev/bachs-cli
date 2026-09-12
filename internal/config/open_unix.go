//go:build !windows

package config

import (
	"os"
	"syscall"
)

// openSecure opens the config file for writing, refusing to follow a symlink.
//
// Without O_NOFOLLOW, an attacker who can create a file in the config
// directory could plant a link and have the API key written through it to
// somewhere they can read. The 0700 directory mode makes that hard for a
// non-owner, but this closes it outright.
func openSecure(path string) (*os.File, error) {
	return os.OpenFile(
		path,
		os.O_WRONLY|os.O_CREATE|os.O_TRUNC|syscall.O_NOFOLLOW,
		0o600,
	)
}
