package ws

import (
	"errors"
	"io"
	"net"
	"testing"
	"time"
)

type dummyListener struct {
	accepter func() (net.Conn, error)
}

func (l *dummyListener) Accept() (net.Conn, error) {
	return l.accepter()
}
func (l *dummyListener) Close() error {
	return nil
}

func (l *dummyListener) Addr() net.Addr {
	return nil
}

func TestAccept(t *testing.T) {
	ch := make(chan bool)
	srv := &server{
		listener: &dummyListener{
			accepter: func() (net.Conn, error) {
				return nil, errors.New("Accept failed")
			},
		},
		quitCh: ch,
	}

	ah := func(err error, s Socket) {
		if err == nil {
			t.Error("Accept handler not called with an error")
		}
		srv.Close()
	}

	srv.acceptHandler = ah
	srv.acceptLoop()
}

type testConnection struct {
	io.ReadWriter
}

func (tc testConnection) Close() error {
	return nil
}
func (tc testConnection) LocalAddr() net.Addr {
	return nil
}
func (tc testConnection) RemoteAddr() net.Addr {
	return nil
}
func (tc testConnection) SetDeadline(t time.Time) error {
	return nil
}
func (tc testConnection) SetReadDeadline(t time.Time) error {
	return nil
}
func (tc testConnection) SetWriteDeadline(t time.Time) error {
	return nil
}
