package ws

import (
	"bufio"
	"crypto/sha1"
	"encoding/base64"
	"fmt"
	"net"
	"strings"
)

const ACCEPT_KEY_SUFFIX = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

type AcceptHandler func(error, Socket)
type Server interface {
	Listen(url string, handler AcceptHandler) error
	Close() error
}

type server struct {
	listener      net.Listener
	acceptHandler AcceptHandler
	quitCh        chan bool
}

func (s *server) Listen(url string, handler AcceptHandler) error {

	if ln, err := net.Listen("tcp", url); err != nil {
		return err
	} else {
		defer ln.Close()
		s.listener = ln
		s.acceptHandler = handler
		s.acceptLoop()
	}

	return nil
}

func (s *server) Close() error {
	close(s.quitCh)
	return nil
}

func (s *server) acceptLoop() {

	for {
		if conn, err := s.listener.Accept(); err != nil {
			s.acceptHandler(err, &socket{})
		} else {

			c := &socket{
				conn: conn,
				frameHandler: func(messsageType byte, payload []byte, fin bool) {
				},
				serverQuit: s.quitCh,
				status:     SocketStatusOpening,
			}
			err = c.handshake()
			if err != nil {
				fmt.Println(err)
				conn.Close()
			} else {
				s.acceptHandler(nil, c)
			}
		}
	}
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

func scanHeaders(scanner *bufio.Scanner) map[string]string {
	headers := map[string]string{}

	for {
		scanner.Scan()
		line := scanner.Text()
		if line == "" {
			break
		}

		headerParts := strings.Split(line, ":")
		headerKey := strings.ToLower(headerParts[0])
		headerValue := strings.Trim(headerParts[1], " ")
		headers[headerKey] = headerValue
	}
	return headers
}
