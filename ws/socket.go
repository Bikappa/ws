package ws

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

type Socket interface {
	Run()
	OnText(h TextHandler)
	OnBinary(h BinaryHandler)
	OnStreamStart(h StreamStartHandler)
	SendMessage(messageType byte, r io.Reader)
	Close() error
	Status() int
}

type MessageType byte
type FrameHandler func(byte, []byte, bool)
type TextHandler func(text string)
type BinaryHandler func(data []byte)
type StreamStartHandler func(t MessageType, r io.Reader)

const (
	OPCODE_CONTINUATION = 0x0
	OPCODE_TEXT         = 0x1
	OPCODE_BINARY       = 0x2
	OPCODE_CLOSE        = 0x8
	OPCODE_PING         = 0x9
	OPCODE_PONG         = 0xa
)

const (
	CloseCodeNormal               = uint16(1000)
	CloseCodeGoingAway            = uint16(1001)
	CloseCodeProtocolError        = uint16(1002)
	CloseCodeUnacceptableMessage  = uint16(1003)
	CloseCodeInconsistentData     = uint16(1007)
	CloseCodePolicyViolated       = uint16(1008)
	CloseCodeMessageTooBig        = uint16(1009)
	CloseCodeUnsupportedExtension = uint16(1010)
	CloseCodeUnexpectedCondition  = uint16(1011)
)

var validCloseCodes map[uint16]bool = map[uint16]bool{
	CloseCodeNormal:               true,
	CloseCodeGoingAway:            true,
	CloseCodeProtocolError:        true,
	CloseCodeUnacceptableMessage:  true,
	CloseCodeInconsistentData:     true,
	CloseCodePolicyViolated:       true,
	CloseCodeMessageTooBig:        true,
	CloseCodeUnsupportedExtension: true,
	CloseCodeUnexpectedCondition:  true,
}

const (
	MESSAGE_TYPE_TEXT   = OPCODE_TEXT
	MESSAGE_TYPE_BINARY = OPCODE_BINARY
)

const (
	SocketStatusOpening = 1
	SocketStatusOpen    = 2
	SocketStatusClosing = 3
	SocketStatusClosed  = 4
)

type socket struct {
	rwc                io.ReadWriteCloser
	frameHandler       FrameHandler
	textHandler        TextHandler
	binaryHandler      BinaryHandler
	streamStartHandler StreamStartHandler
	streamWriter       io.WriteCloser
	fragmentedMsgType  byte
	danglingUTF8Bytes  []byte
	serverQuit         chan bool
	status             int
}

type frame struct {
	Fin     bool
	Rsvs    []bool
	Opcode  byte
	Payload []byte
}

func (s *socket) Run() {
	go func() {
		<-s.serverQuit
		s.Close()
		return
	}()
	s.readLoop()
	s.Close()
}

func (s *socket) readLoop() {
	for {
		f, err := s.readFrame()

		if err != nil {
			fmt.Println("err1", err.Error())
			switch err {
			case io.EOF:
				// connection dropped, nothing we can do here
				s.status = SocketStatusClosed
			case ErrInvalidUTF8:
				s.sendCloseWithCode(CloseCodeInconsistentData)
				s.status = SocketStatusClosing
			default:
				s.sendCloseWithCode(CloseCodeProtocolError)
				s.status = SocketStatusClosing
			}
			return
		}
		fmt.Printf("RX Fin=%t Opcode=%d Len=%d\n", f.Fin, f.Opcode, len(f.Payload))

		switch f.Opcode {
		case byte(OPCODE_TEXT):
			s.frameHandler(f.Opcode, f.Payload, f.Fin)
			if f.Fin && s.textHandler != nil {
				s.textHandler(string(f.Payload))
			}
		case byte(OPCODE_BINARY):
			s.frameHandler(f.Opcode, f.Payload, f.Fin)
			if f.Fin && s.binaryHandler != nil {
				s.binaryHandler(f.Payload)
			}
		case byte(OPCODE_CONTINUATION):
			s.frameHandler(f.Opcode, f.Payload, f.Fin)
		case byte(OPCODE_PING):
			s.sendPong(f.Payload)
		case byte(OPCODE_CLOSE):
			if s.status != SocketStatusClosing {
				s.sendClose(ensureValidCloseCode(f.Payload))
				s.status = SocketStatusClosed
			}
		}

		if f.Opcode == byte(OPCODE_CLOSE) {
			return
		}

		if f.Fin && f.Opcode == OPCODE_CONTINUATION {
			s.streamWriter.Close()
			s.streamWriter = nil
		}

		if !f.Fin && f.Opcode != OPCODE_CONTINUATION {
			var r io.Reader
			var w io.WriteCloser
			if s.streamStartHandler != nil {
				r, w = io.Pipe()
			} else {
				w = WriterNopCloser{io.Discard}
			}
			s.streamWriter = w
			if s.streamStartHandler != nil {
				s.streamStartHandler(MessageType(f.Opcode), r)
			}
			w.Write(f.Payload)
		}
	}
}

func (s *socket) Close() error {

	for {
		switch s.status {
		case SocketStatusClosed:
			return s.rwc.Close()
		case SocketStatusClosing:
			// If we were nice we would wait for the endpoint to reply our close message
			// Unfortunately this leads to some complications
			// For example, we need to read the offending message entirely before we can
			// read the close reply (or any other frame)
			s.status = SocketStatusClosed
		default:
			closeCode := make([]byte, 2)
			binary.BigEndian.PutUint16(closeCode, CloseCodeGoingAway)
			s.sendClose(closeCode)
			s.status = SocketStatusClosing
		}

		if s.status != SocketStatusClosing {
			break
		}
	}
	return s.rwc.Close()
}

func (s *socket) sendPong(payload []byte) {
	data := createMessageFrame(bytes.NewReader(payload), uint64(len(payload)), OPCODE_PONG, true)
	// TODO: handle error
	s.rwc.Write(data)
}

func (s *socket) sendCloseWithCode(code uint16) {
	closeCode := make([]byte, 2)
	binary.BigEndian.PutUint16(closeCode, code)
	s.sendClose(closeCode)
}

func (s *socket) sendClose(payload []byte) {
	s.sendFrame(OPCODE_CLOSE, bytes.NewReader(payload), true)
}

func (s *socket) sendFrame(messageType byte, r io.Reader, fin bool) error {
	buf := bytes.NewBuffer([]byte{})
	io.Copy(buf, r)

	fmt.Printf("TX Fin=%t Opcode=%d Len=%d\n", fin, messageType, buf.Len())
	data := createMessageFrame(bytes.NewReader(buf.Bytes()), uint64(buf.Len()), messageType, fin)
	_, err := s.rwc.Write(data)
	return err
}

func (s *socket) OnFrame(h FrameHandler) {
	s.frameHandler = h
}

func (s *socket) OnText(h TextHandler) {
	s.textHandler = h
}

func (s *socket) OnBinary(h BinaryHandler) {
	s.binaryHandler = h
}

func (s *socket) OnStreamStart(h StreamStartHandler) {
	s.streamStartHandler = h
}

func (s *socket) SendMessage(t byte, r io.Reader) {
	panic("Not implemented")
}

func (c *socket) handshake() error {

	scanner := bufio.NewScanner(c.rwc)

	// Read status line
	scanner.Scan()
	headers := scanHeaders(scanner)
	if value, ok := headers["connection"]; !ok || strings.ToLower(value) != "upgrade" {
		c.status = SocketStatusClosed
		return ErrMissingUpgrade
	}

	if value, ok := headers["upgrade"]; !ok || strings.ToLower(value) != "websocket" {
		c.status = SocketStatusClosed
		return ErrInvalidUpgrade
	}

	if _, ok := headers["sec-websocket-key"]; !ok {
		c.status = SocketStatusClosed
		return ErrInvalidWebsocketKey
	}

	acceptKey := generateWebsocketAccept(headers["sec-websocket-key"])

	responseMessage := strings.Join([]string{
		"HTTP/1.1 101 Switching Protocols",
		"Connection: Upgrade",
		"Upgrade: websocket",
		fmt.Sprintf("Sec-WebSocket-Accept: %s", acceptKey),
		"\r\n",
	}, "\r\n")

	if _, err := c.rwc.Write([]byte(responseMessage)); err != nil {
		c.status = SocketStatusClosed
		return err
	}

	c.status = SocketStatusOpen
	return nil
}

func (s *socket) Status() int {
	return s.status
}

func (s *socket) readFrame() (*frame, error) {
	frameStart, err := readAll(s.rwc, 2)
	if err != nil {
		return nil, err
	}
	flags := frameStart[0]
	fin := flags&0x80 != 0
	rsv1 := flags&0x40 != 0
	rsv2 := flags&0x20 != 0
	rsv3 := flags&0x10 != 0

	if rsv1 || rsv2 || rsv3 {
		return nil, ErrInvalidRSV
	}
	opcode := flags & 0x0f

	// non-control frames (0 first bit) higher than 2 are reserved
	// control frames (1 first bit) higher than 10 are reserved
	isReservedOpcode := (opcode & 0x7) > 2

	if isReservedOpcode {
		return nil, ErrReservedOpcode
	}
	continuationExpected := s.streamWriter != nil
	if !isControlFrame(opcode) {
		if continuationExpected && opcode != OPCODE_CONTINUATION {
			return nil, ErrInvalidOpcode
		}

		if !continuationExpected && opcode == OPCODE_CONTINUATION {
			return nil, ErrInvalidContinuation
		}

		if opcode != OPCODE_CONTINUATION {
			s.fragmentedMsgType = opcode
		}
	}

	secondByte := frameStart[1]
	mask := secondByte&0x80 != 0
	if !mask {
		return nil, ErrUnmaskedframe
	}

	if isControlFrame(opcode) && !fin {
		return nil, ErrFragmentedControlFrame
	}
	payloadLength, err := s.resolvePayloadLength(secondByte&0x7f, isControlFrame(opcode))
	if err != nil {
		return nil, err
	}
	maskingKey, err := readAll(s.rwc, 4)
	if err != nil {
		return nil, err
	}
	payloadData, err := readAll(s.rwc, payloadLength)
	if err != nil {
		return nil, err
	}
	unmasked := unmaskData(payloadData, maskingKey)

	if s.fragmentedMsgType == OPCODE_TEXT && !isControlFrame(opcode) {
		err = s.validTextFragment(unmasked, fin)
		if err != nil {
			return &frame{}, err
		}
	}

	if opcode == OPCODE_CLOSE && payloadLength > 2 && !utf8.Valid(unmasked[2:]) {
		return nil, ErrInvalidClosePayload
	}

	return &frame{
		Fin:     fin,
		Rsvs:    []bool{rsv1, rsv2, rsv3},
		Opcode:  opcode,
		Payload: unmasked,
	}, nil
}

func (s *socket) validTextFragment(payload []byte, fin bool) error {
	payload = append(s.danglingUTF8Bytes, payload...)
	end := len(payload)
	for {

		if fin {
			// last fragment can't have a dangling invalid utf8 code
			s.danglingUTF8Bytes = []byte{}
			break
		}

		r, _ := utf8.DecodeLastRune(payload[:end])
		if r != utf8.RuneError || end == 0 {
			dangling := payload[end:]
			s.danglingUTF8Bytes = dangling
			break
		}
		end = end - 1
		if end < len(payload)-3 {
			// this fragment is invalid on its own
			return ErrInvalidUTF8
		}
	}
	// We check the validity exluded the possibly dangling code
	if !utf8.Valid(payload[0:end]) {
		return ErrInvalidUTF8
	}

	// also if the first dangling byte cannot start a rune there's no point continuing
	if end != len(payload) && !utf8.RuneStart(payload[end]) {
		return ErrInvalidUTF8
	}

	return nil
}

func (s *socket) resolvePayloadLength(firstLengthByte byte, isControlFrame bool) (uint64, error) {
	if firstLengthByte <= 125 {
		return uint64(firstLengthByte), nil
	}

	if isControlFrame {
		return 0, errors.New("INVALID CONTROL FRAME. PAYLOAD TOO LONG")
	}

	nBytes := 2
	if firstLengthByte == 127 {
		nBytes = 8
	}
	payloadLength := uint64(0)
	lengthBytes, err := readAll(s.rwc, uint64(nBytes))
	if err != nil {
		return 0, err
	}
	for i := nBytes - 1; i >= 0; i-- {
		b := lengthBytes[nBytes-1-i]
		payloadLength = payloadLength | (uint64(b) << (i * 8))
	}
	return payloadLength, nil
}

func createMessageFrame(r io.Reader, payloadLength uint64, opCode byte, fin bool) []byte {
	start := byte(0x00)
	if fin {
		start = 0x80
	}
	header := []byte{
		start | opCode,
	}
	var payloadLengthBytes int
	if payloadLength <= 125 {
		header = append(header, uint8(payloadLength))
	} else {
		if payloadLength <= uint64(0xffff) {
			header = append(header, uint8(126))
			payloadLengthBytes = 2
		} else {
			header = append(header, uint8(127))
			payloadLengthBytes = 8
		}

		for i := payloadLengthBytes - 1; i >= 0; i-- {
			header = append(header, uint8(payloadLength>>(i*8)))
		}
	}

	payload, _ := readAll(r, payloadLength)

	frame := make([]byte, len(header)+int(payloadLength))
	copy(frame, header)

	payloadSlice := frame[len(header):]
	copy(payloadSlice, payload)

	return frame
}

func unmaskData(data []byte, mask []byte) []byte {
	unmasked := make([]byte, len(data))
	for i, b := range data {
		unmasked[i] = b ^ mask[i%4]
	}
	return unmasked
}

func readAll(r io.Reader, length uint64) ([]byte, error) {
	if length == 0 {
		return []byte{}, nil
	}
	b := make([]byte, length)
	received := 0
	for {
		missing := b[received:]
		n, err := r.Read(missing)
		received += n
		if err != nil {
			// TODO: handle this (close the connection)
			return nil, err
		}
		if n == len(missing) {
			break
		}
	}
	return b, nil
}

func isControlFrame(opcode byte) bool {
	return (opcode & 0x8) == 0x8
}

func ensureValidCloseCode(payload []byte) []byte {
	if len(payload) == 0 {
		return payload
	}
	forcedCloseCode := make([]byte, 2)
	if len(payload) == 1 {
		binary.BigEndian.PutUint16(forcedCloseCode, CloseCodeProtocolError)
		return forcedCloseCode
	}

	receivedCloseCode := binary.BigEndian.Uint16(payload[0:2])
	if _, ok := validCloseCodes[receivedCloseCode]; ok {
		return payload
	}

	if receivedCloseCode >= 3000 && receivedCloseCode < 5000 {
		return payload
	}

	binary.BigEndian.PutUint16(forcedCloseCode, CloseCodeProtocolError)
	return forcedCloseCode
}
