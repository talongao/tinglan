//go:build windows

package main

import "os/exec"

func openExternalURL(target string) error {
	return exec.Command("rundll32", "url.dll,FileProtocolHandler", target).Start()
}
