package ws

import (
	"bufio"
	"crypto/sha1"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
)

const ACCEPT_KEY_SUFFIX = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"
const (
	MESSAGE_TYPE_TEXT   = 1
	MESSAGE_TYPE_BINARY = 2
)

type MessageHandler func(byte, io.Reader)
type Socket interface {
	OnMessage(handler MessageHandler)
	SendMessage(messageType byte, r io.Reader) error
	Close() error
}

type AcceptHandler func(error, Socket)
type Server interface {
	Listen(url string, handler AcceptHandler) error
}

type server struct {
	listener      net.Listener
	acceptHandler AcceptHandler
}

func (s *server) Listen(url string, handler AcceptHandler) error {

	if ln, err := net.Listen("tcp", url); err != nil {
		return err
	} else {
		s.listener = ln
		s.acceptHandler = handler
		s.acceptLoop()
	}

	return nil
}

func (s *server) acceptLoop() {
	for {
		if conn, err := s.listener.Accept(); err != nil {
			s.acceptHandler(err, &socket{})
		} else {
			err = s.handshake(conn)
			if err != nil {
				conn.Close()
			}
			c := &socket{
				conn: conn,
				messageHandler: func(messsageType byte, r io.Reader) {
				},
			}
			go c.readLoop()

			s.acceptHandler(nil, c)
		}
	}
}

func (s *server) handshake(rw io.ReadWriter) error {
	scanner := bufio.NewScanner(rw)

	// Read status line
	scanner.Scan()
	headers := map[string]string{}

	for {
		scanner.Scan()
		line := scanner.Text()
		if line == "" {
			break
		}

		headerParts := strings.Split(line, ": ")
		headers[headerParts[0]] = headerParts[1]
	}

	if value, ok := headers["Connection"]; !ok || value != "Upgrade" {
		return errors.New("MISSING UPGRADE")
	}

	if value, ok := headers["Upgrade"]; !ok || value != "websocket" {
		return errors.New("INVALID UPGRADE")
	}

	acceptKey := generateWebsocketAccept(headers["Sec-WebSocket-Key"])
	responseMessage := fmt.Sprintf(
		`HTTP/1.1 101 Switching Protocols
Connection: Upgrade
Upgrade: websocket
Sec-WebSocket-Accept: %s

`, acceptKey)
	if _, err := rw.Write([]byte(responseMessage)); err != nil {
		return err
	}
	return nil
}

func generateWebsocketAccept(key string) string {
	concatenation := fmt.Sprintf("%s%s", key, ACCEPT_KEY_SUFFIX)

	h := sha1.New()
	fmt.Fprint(h, concatenation)
	sha := h.Sum(nil)
	return base64.StdEncoding.EncodeToString(sha)
}

func NewServer() Server {
	return &server{}
}
