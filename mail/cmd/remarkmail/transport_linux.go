//go:build linux

package main

import (
	"encoding/binary"
	"fmt"
	"syscall"

	"remarkable-mail/internal/mailapp"
)

func runAppLoad(app *mailapp.App, path string) error {
	fd, err := syscall.Socket(syscall.AF_UNIX, syscall.SOCK_SEQPACKET, 0)
	if err != nil {
		return err
	}
	defer syscall.Close(fd)
	if err = syscall.Connect(fd, &syscall.SockaddrUnix{Name: path}); err != nil {
		return err
	}
	for {
		header := make([]byte, 8)
		n, _, flags, _, err := syscall.Recvmsg(fd, header, nil, 0)
		if err != nil {
			return err
		}
		if n == 0 {
			return nil
		}
		if n != 8 || flags&syscall.MSG_TRUNC != 0 {
			return fmt.Errorf("Invalid AppLoad header")
		}
		kind := binary.LittleEndian.Uint32(header[:4])
		size := binary.LittleEndian.Uint32(header[4:])
		if size > 10485760 {
			return fmt.Errorf("AppLoad packet too large")
		}
		body := make([]byte, size)
		if size > 0 {
			n, _, flags, _, err = syscall.Recvmsg(fd, body, nil, 0)
			if err != nil {
				return err
			}
			if n != int(size) || flags&syscall.MSG_TRUNC != 0 {
				return fmt.Errorf("Invalid AppLoad body")
			}
		}
		if kind == 0xffffffff {
			return nil
		}
		if kind != 100 {
			continue
		}
		out := respond(app, body)
		binary.LittleEndian.PutUint32(header[:4], 101)
		binary.LittleEndian.PutUint32(header[4:], uint32(len(out)))
		if n, err = syscall.Write(fd, header); err != nil || n != 8 {
			return fmt.Errorf("Could not send AppLoad header")
		}
		if n, err = syscall.Write(fd, out); err != nil || n != len(out) {
			return fmt.Errorf("Could not send AppLoad reply")
		}
	}
}
