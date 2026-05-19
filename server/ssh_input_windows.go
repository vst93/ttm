//go:build windows

package server

import (
	"errors"
	"io"
	"os"
	"unicode/utf16"

	"github.com/muesli/cancelreader"
	"golang.org/x/sys/windows"
)

func startStdinCopy(stdinPipe io.WriteCloser) (<-chan error, func(), error) {
	stdin := windows.Handle(os.Stdin.Fd())
	var mode uint32
	if err := windows.GetConsoleMode(stdin, &mode); err != nil {
		return startCancelableStdinCopy(stdinPipe)
	}

	done := make(chan error, 1)
	go func() {
		buf := make([]uint16, 256)
		for {
			var n uint32
			err := windows.ReadConsole(stdin, &buf[0], uint32(len(buf)), &n, nil)
			if err != nil {
				if errors.Is(err, windows.ERROR_OPERATION_ABORTED) {
					done <- nil
				} else {
					done <- err
				}
				_ = stdinPipe.Close()
				return
			}
			if n == 0 {
				continue
			}
			text := string(utf16.Decode(buf[:n]))
			if _, err := io.WriteString(stdinPipe, text); err != nil {
				done <- err
				_ = stdinPipe.Close()
				return
			}
		}
	}()

	cancel := func() {
		_ = windows.CancelIoEx(stdin, nil)
	}
	return done, cancel, nil
}

func startCancelableStdinCopy(stdinPipe io.WriteCloser) (<-chan error, func(), error) {
	stdinReader, err := cancelreader.NewReader(os.Stdin)
	if err != nil {
		return nil, nil, err
	}

	done := make(chan error, 1)
	go func() {
		_, copyErr := io.Copy(stdinPipe, stdinReader)
		done <- copyErr
		_ = stdinPipe.Close()
	}()

	cancel := func() {
		stdinReader.Cancel()
		_ = stdinReader.Close()
	}
	return done, cancel, nil
}
