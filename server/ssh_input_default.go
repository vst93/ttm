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
