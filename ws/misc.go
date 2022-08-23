package ws

import "io"

type WriterNopCloser struct {
	io.Writer
}

func (WriterNopCloser) Close() error {
	return nil
}
