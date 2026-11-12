//go:build !linux

package main

import (
	"fmt"
	"remarkable-mail/internal/mailapp"
)

func runAppLoad(app *mailapp.App, path string) error {
	return fmt.Errorf("Use --stdio for desktop preview")
}
