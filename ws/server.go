package ws

import (
	"bufio"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
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
			err = s.handshake(conn)
			if err != nil {
				fmt.Println(err)
				conn.Close()
				continue
			}
			c := &socket{
				conn: conn,
				messageHandler: func(messsageType byte, r io.Reader, fin bool) {
				},
				serverQuit: s.quitCh,
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
	log.Println(scanner.Text())
	headers := scanHeaders(scanner)
	if value, ok := headers["connection"]; !ok || strings.ToLower(value) != "upgrade" {
		return errors.New("MISSING UPGRADE")
	}

	if value, ok := headers["upgrade"]; !ok || strings.ToLower(value) != "websocket" {
		return errors.New("INVALID UPGRADE")
	}

	if _, ok := headers["sec-websocket-key"]; !ok {
		return errors.New("INVALID WEBSOCKET KEY")
	}

	acceptKey := generateWebsocketAccept(headers["sec-websocket-key"])

	responseMessage := strings.Join([]string{
		"HTTP/1.1 101 Switching Protocols",
		"Connection: Upgrade",
		"Upgrade: websocket",
		fmt.Sprintf("Sec-WebSocket-Accept: %s", acceptKey),
		"\r\n",
	}, "\r\n")
	fmt.Println(hex.Dump([]byte(responseMessage)))
	if _, err := rw.Write([]byte(responseMessage)); err != nil {
		return err
	}
	return nil
}

func generateWebsocketAccept(key string) string {
	log.Println("key:", key)
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
		fmt.Println(headerParts)
		headerKey := strings.ToLower(headerParts[0])
		headerValue := strings.Trim(headerParts[1], " ")
		headers[headerKey] = headerValue
	}

	fmt.Println(headers)
	return headers
}
