package ws

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"unicode/utf8"
)

type Socket interface {
	OnText(h TextHandler)
	OnBinary(h BinaryHandler)
	OnStreamStart(h StreamStartHandler)
	SendMessage(messageType byte, r io.Reader)
	Close() error
}

type MessageType uint16
type FrameHandler func(byte, []byte, bool)
type TextHandler func(text string)
type BinaryHandler func(data []byte)
type StreamStartHandler func(t *MessageType, r io.Reader)

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

type socket struct {
	conn              net.Conn
	frameHandler      FrameHandler
	textHandler       TextHandler
	binaryHandler     BinaryHandler
	isContinuing      bool
	fragmentedMsgType byte
	danglingUTF8Bytes []byte
	serverQuit        chan bool
}

type frame struct {
	Fin     bool
	Rsvs    []bool
	Opcode  byte
	Payload []byte
}

func (s *socket) run() {
	readCompleted := make(chan bool)

	go func() {
		s.readLoop()
		readCompleted <- true
	}()

	select {
	case <-readCompleted:
		return
	case <-s.serverQuit:
		s.Close()
		return
	}
}

func (s *socket) readLoop() {
	defer s.conn.Close()
	for {
		f, err := s.readFrame()

		if err != nil {
			fmt.Println(err.Error())
			switch err {
			case io.EOF:
				// connection dropped, nothing we can do here
			case ErrInvalidUTF8:
				s.sendCloseWithCode(CloseCodeInconsistentData)
			default:
				s.sendCloseWithCode(CloseCodeProtocolError)
			}
			return
		}

		switch f.Opcode {
		case byte(MESSAGE_TYPE_TEXT):
			s.frameHandler(f.Opcode, f.Payload, f.Fin)
			if f.Fin && s.textHandler != nil {
				s.textHandler(string(f.Payload))
			}
		case byte(MESSAGE_TYPE_BINARY):
			s.frameHandler(f.Opcode, f.Payload, f.Fin)
			if f.Fin && s.binaryHandler != nil {
				s.binaryHandler(f.Payload)
			}
		case byte(OPCODE_CONTINUATION):
			s.frameHandler(f.Opcode, f.Payload, f.Fin)
		case byte(OPCODE_PING):
			s.sendPong(f.Payload)
		case byte(OPCODE_CLOSE):
			s.sendClose(ensureValidCloseCode(f.Payload))
			return
		}

		if !isControlFrame(f.Opcode) {
			if !f.Fin {
				s.isContinuing = true
			} else {
				s.isContinuing = false
			}
		}
	}
}

func (s *socket) sendPong(payload []byte) {
	data := createMessageFrame(bytes.NewReader(payload), uint64(len(payload)), OPCODE_PONG, true)
	// TODO: handle error
	s.conn.Write(data)
}

func (s *socket) sendCloseWithCode(code uint16) {
	closeCode := make([]byte, 2)
	binary.BigEndian.PutUint16(closeCode, code)
	s.sendClose(closeCode)
}
func (s *socket) sendClose(payload []byte) {
	data := createMessageFrame(bytes.NewReader(payload), uint64(len(payload)), OPCODE_CLOSE, true)
	// TODO: handle error
	s.conn.Write(data)
}

func (s *socket) sendFrame(messageType byte, r io.Reader, fin bool) error {
	buf := make([]byte, 4096)
	var payload []byte
	for {
		n, err := r.Read(buf)
		payload = append(payload, buf[0:n]...)
		if err == io.EOF {
			break
		}
	}
	// TODO: provide option to read all till EOF in creteMessageFrom
	data := createMessageFrame(bytes.NewReader(payload), uint64(len(payload)), messageType, fin)
	// TODO: handle error
	_, err := s.conn.Write(data)
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
	panic("Not implemented")
}

func (s *socket) SendMessage(t byte, r io.Reader) {
	panic("Not implemented")
}

func (s *socket) Close() error {
	closeCode := make([]byte, 2)
	binary.BigEndian.PutUint16(closeCode, CloseCodeGoingAway)
	s.sendClose(closeCode)
	return s.conn.Close()
}

var (
	ErrInvalidRSV             = errors.New("INVALID RSV")
	ErrInvalidOpcode          = errors.New("INVALID OPCODE")
	ErrReservedOpcode         = errors.New("RESERVED OPCODE")
	ErrInvalidContinuation    = errors.New("INVALID CONTINUATION FRAME")
	ErrUnmaskedframe          = errors.New("UNMASKED FRAME")
	ErrFragmentedControlFrame = errors.New("FRAGMENTED CONTROL FRAME")
	ErrInvalidClosePayload    = errors.New("INVALID CLOSE PAYLOAD")
	ErrInvalidUTF8            = errors.New("INVALID UTF8")
)

func (s *socket) readFrame() (*frame, error) {
	frameStart, err := readAll(s.conn, 2)
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

	if s.isContinuing && opcode != OPCODE_CONTINUATION && !isControlFrame(opcode) {
		return nil, ErrInvalidOpcode
	}

	if !s.isContinuing && opcode == OPCODE_CONTINUATION {
		return nil, ErrInvalidContinuation
	}

	if !isControlFrame(opcode) && opcode != OPCODE_CONTINUATION {
		s.fragmentedMsgType = opcode
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
	maskingKey, err := readAll(s.conn, 4)
	if err != nil {
		return nil, err
	}
	payloadData, err := readAll(s.conn, payloadLength)
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
	lengthBytes, err := readAll(s.conn, uint64(nBytes))
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
