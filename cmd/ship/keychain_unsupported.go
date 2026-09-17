//go:build !darwin || !cgo

package main

import "errors"

var errKeychainUnsupported = errors.New("local secret storage currently requires macOS with a cgo-enabled Ship build; no plaintext fallback")

func keychainPut(string, []byte) error   { return errKeychainUnsupported }
func keychainGet(string) ([]byte, error) { return nil, errKeychainUnsupported }
func keychainDelete(string) error        { return errKeychainUnsupported }
