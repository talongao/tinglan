//go:build darwin

package main

import "os/exec"

// openExternalURL opens a documentation or download page in the user's browser.
// It is only used for guidance links, never for the login flow itself.
func openExternalURL(target string) error {
	return exec.Command("open", target).Start()
}
