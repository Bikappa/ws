package ws

import (
	"errors"
	"net"
	"testing"
)

type dummyListener struct{}

func (l *dummyListener) Accept() (net.Conn, error) {
	return nil, errors.New("Accept failed")
}
func (l *dummyListener) Close() error {
	return nil
}

func (l *dummyListener) Addr() net.Addr {
	return nil
}

func TestAccept(t *testing.T) {
	called := false
	ch := make(chan bool)
	srv := &server{
		listener: &dummyListener{},
		quitCh:   ch,
	}

	ah := func(err error, s Socket) {
		called = true
		srv.Close()
	}

	srv.acceptHandler = ah

	srv.acceptLoop()
	if !called {
		t.Error("Accept handler not called")
	}
}
