//go:build !windows

package server

import (
	"io"
	"os"

	"github.com/muesli/cancelreader"
)

func startStdinCopy(stdinPipe io.WriteCloser) (<-chan error, func(), error) {
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
