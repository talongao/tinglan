//go:build !darwin && !windows

package main

import "os/exec"

func openExternalURL(target string) error {
	return exec.Command("xdg-open", target).Start()
}
