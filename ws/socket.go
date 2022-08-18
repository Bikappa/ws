package ws

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"unicode/utf8"
)

const (
	OPCODE_CONTINUATION = 0x0
	OPCODE_TEXT         = 0x1
	OPCODE_BINARY       = 0x2
	OPCODE_CLOSE        = 0x8
	OPCODE_PING         = 0x9
	OPCODE_PONG         = 0xa
)
const (
	MESSAGE_TYPE_TEXT   = OPCODE_TEXT
	MESSAGE_TYPE_BINARY = OPCODE_BINARY
)

type socket struct {
	conn              net.Conn
	messageHandler    MessageHandler
	isContinuoning    bool
	fragmentedMsgType byte
	danglingUTF8Bytes []byte
}

type frame struct {
	Fin     bool
	Rsvs    []bool
	Opcode  byte
	Payload []byte
}

func (s *socket) readLoop() {
	defer s.Close()
	for {
		f, err := s.readFrame()

		if err != nil {
			fmt.Println(err.Error())
			return
		}

		switch f.Opcode {
		case byte(MESSAGE_TYPE_TEXT):
			s.messageHandler(f.Opcode, bytes.NewReader(f.Payload), f.Fin)
		case byte(MESSAGE_TYPE_BINARY):
			s.messageHandler(f.Opcode, bytes.NewReader(f.Payload), f.Fin)
		case byte(OPCODE_CONTINUATION):
			s.messageHandler(f.Opcode, bytes.NewReader(f.Payload), f.Fin)
		case byte(OPCODE_PING):
			s.sendPong(f.Payload)
		case byte(OPCODE_CLOSE):
			fmt.Println("CLOSING")
			s.sendClose(f.Payload)
			return
		}

		if !isControlFrame(f.Opcode) {
			if !f.Fin {
				s.isContinuoning = true
			} else {
				s.isContinuoning = false
			}
		}
	}
}

func (s *socket) sendPong(payload []byte) {
	data := createMessageFrame(bytes.NewReader(payload), uint64(len(payload)), OPCODE_PONG, true)
	// TODO: handle error
	s.conn.Write(data)
}

func (s *socket) sendClose(payload []byte) {
	data := createMessageFrame(bytes.NewReader(payload), uint64(len(payload)), OPCODE_CLOSE, true)
	// TODO: handle error
	s.conn.Write(data)
}

func (s *socket) SendMessage(messageType byte, r io.Reader, fin bool) error {
	log.Default().Println("Sending frame")
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
	fmt.Println(hex.Dump(data))
	// TODO: handle error
	_, err := s.conn.Write(data)
	return err
}

func (s *socket) OnMessage(handler MessageHandler) {
	s.messageHandler = handler
}

func (cl *socket) Close() error {
	return cl.conn.Close()
}

func (s *socket) readFrame() (*frame, error) {
	frameStart := readAll(s.conn, 2)
	fmt.Println("Frame start")
	fmt.Println(hex.Dump(frameStart))
	flags := frameStart[0]
	fin := flags&0x80 != 0
	rsv1 := flags&0x40 != 0
	rsv2 := flags&0x20 != 0
	rsv3 := flags&0x10 != 0

	if rsv1 || rsv2 || rsv3 {
		return nil, errors.New("INVALID RSV")
	}
	opcode := flags & 0x0f

	// non-control frames (0 first bit) higher than 2 are reserved
	// control frames (1 first bit) higher than 10 are reserved
	isReservedOpcode := (opcode & 0x7) > 2

	if isReservedOpcode {
		return nil, errors.New("RESERVED OPCODE")
	}

	if s.isContinuoning && opcode != OPCODE_CONTINUATION && !isControlFrame(opcode) {
		return nil, errors.New("INVALID OPCODE")
	}

	if !s.isContinuoning && opcode == OPCODE_CONTINUATION {
		return nil, errors.New("INVALID CONTINUATION FRAME")
	}

	if !isControlFrame(opcode) && opcode != OPCODE_CONTINUATION {
		s.fragmentedMsgType = opcode
	}

	secondByte := frameStart[1]
	mask := secondByte&0x80 != 0
	if !mask {
		return nil, errors.New("UNMASKED FRAME")
	}

	if isControlFrame(opcode) && !fin {
		return nil, errors.New("FRAGMENTED CONTROL FRAME")
	}
	payloadLength, err := s.resolvePayloadLength(secondByte&0x7f, isControlFrame(opcode))
	fmt.Println("payloadLength:", payloadLength)
	if err != nil {
		return nil, err
	}
	maskingKey := readAll(s.conn, 4)
	payloadData := readAll(s.conn, payloadLength)
	unmasked := unmaskData(payloadData, maskingKey)

	if s.fragmentedMsgType == OPCODE_TEXT {
		err = s.validTextFragment(unmasked, fin)
		if err != nil {
			return &frame{}, err
		}
	}

	if fin && !isControlFrame(opcode) {
		s.danglingUTF8Bytes = []byte{}
	}

	return &frame{
		Fin:     fin,
		Rsvs:    []bool{rsv1, rsv2, rsv3},
		Opcode:  opcode,
		Payload: unmasked,
	}, nil
}

func (s *socket) validTextFragment(payload []byte, fin bool) error {
	// append the dangling bytes from previews frame
	if len(s.danglingUTF8Bytes) > 0 {
		fmt.Println("Using previous fragment dangling bytes")
		fmt.Println(hex.EncodeToString(s.danglingUTF8Bytes))
	}
	payload = append(s.danglingUTF8Bytes, payload...)
	end := len(payload)
	for {

		if fin {
			// last fragment can't have a dangling invalid utf8 code
			break
		}

		r, runeLength := utf8.DecodeLastRune(payload[0:end])
		if r != utf8.RuneError || end == 0 {
			dangling := payload[end-runeLength : end]
			if len(dangling) > 0 {
				fmt.Println("Saving possible dangling utf8 code", hex.EncodeToString(dangling))
				s.danglingUTF8Bytes = dangling
			}
			break
		}
		end = end - 1
		if end < len(payload)-3 {
			fmt.Println("Too long invalid suffix")
			// this fragment is invalid on its own
			return errors.New("INVALID TEXT PAYLOAD")
		}
	}

	// We check the validity exluded the possibly dangling code
	fmt.Println("Validation", hex.EncodeToString(payload[0:end]))
	if !utf8.Valid(payload[0:end]) {
		fmt.Println("Invalid fragment payload")
		fmt.Println(payload[0:end], s.danglingUTF8Bytes, fin)
		return errors.New("INVALID TEXT PAYLOAD")
	}

	// also if the first dangling byte cannot start a rune there's no point continuing
	if end != len(payload) && !utf8.RuneStart(payload[end]) {
		fmt.Println("Not a rune start")
		return errors.New("INVALID TEXT PAYLOAD")
	}

	return nil
}

func (s *socket) resolvePayloadLength(firstLengthByte byte, isControlFrame bool) (uint64, error) {
	if firstLengthByte <= 125 {
		return uint64(firstLengthByte), nil
	}

	if isControlFrame {
		return 0, errors.New("Invalid control frame. Payload too long")
	}

	nBytes := 2
	if firstLengthByte == 127 {
		nBytes = 8
	}
	payloadLength := uint64(0)
	lengthBytes := readAll(s.conn, uint64(nBytes))
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

	payload := readAll(r, payloadLength)

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

func readAll(r io.Reader, length uint64) []byte {
	if length == 0 {
		return []byte{}
	}
	b := make([]byte, length)
	received := 0
	for {
		missing := b[received:]
		n, err := r.Read(missing)
		received += n
		if err != nil {
			// TODO: handle this (close the connection)
			panic(err)
		}
		if n == len(missing) {
			break
		}
	}
	return b
}

func isControlFrame(opcode byte) bool {
	return (opcode & 0x8) == 0x8
}
