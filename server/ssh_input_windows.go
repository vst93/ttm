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

// stdinReaderAdapter wraps cancelreader.CancelReader to satisfy io.ReadCloser.
type stdinReaderAdapter struct {
	cr cancelreader.CancelReader
}

func (a *stdinReaderAdapter) Read(p []byte) (int, error) {
	return a.cr.Read(p)
}

func (a *stdinReaderAdapter) Close() error {
	a.cr.Cancel()
	return a.cr.Close()
}

func newStdinReader() (io.ReadCloser, error) {
	cr, err := cancelreader.NewReader(os.Stdin)
	if err != nil {
		return nil, err
	}
	return &stdinReaderAdapter{cr: cr}, nil
}

// Cancel interrupts any pending Read call without closing the reader.
func cancelStdinReader(r io.ReadCloser) {
	if cr, ok := r.(*stdinReaderAdapter); ok {
		cr.cr.Cancel()
		return
	}
	_ = r.Close()
}
