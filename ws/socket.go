package ws

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
)

type socket struct {
	conn           net.Conn
	messageHandler MessageHandler
}

type frame struct {
	Fin     bool
	Rsvs    []bool
	Opcode  byte
	Payload []byte
}

func (s *socket) readLoop() {
	for {
		f, err := s.readFrame()

		if err != nil {
			fmt.Println(err.Error())
		}

		switch f.Opcode {
		case byte(MESSAGE_TYPE_TEXT):
			s.messageHandler(f.Opcode, bytes.NewReader(f.Payload))
		}
	}
}

func (s *socket) SendMessage(messageType byte, r io.Reader) error {
	panic("Not implemented")
	return nil
}

func (s *socket) OnMessage(handler MessageHandler) {
	s.messageHandler = handler
}

func (cl *socket) Close() error {
	// TODO: send close message
	return cl.conn.Close()
}

func (s *socket) readFrame() (*frame, error) {
	frameStart := make([]byte, 2)
	if _, err := s.conn.Read(frameStart); err != nil {
		fmt.Println(err)
		return nil, errors.New("Invalid frame")
	} else {
		flags := frameStart[0]
		fin := flags&0x80 != 0
		rsv1 := flags&0x40 != 0
		rsv2 := flags&0x20 != 0
		rsv3 := flags&0x10 != 0
		opcode := flags & 0x0f

		secondByte := frameStart[1]
		mask := secondByte&0x80 != 0
		if !mask {
			return nil, errors.New("Unmasked frame")
		}

		payloadLength := s.resolvePayloadLength(secondByte & 0x7f)

		maskingKey := make([]byte, 4)
		s.conn.Read(maskingKey)

		payloadData := make([]byte, payloadLength)
		s.conn.Read(payloadData)
		unmasked := unmaskData(payloadData, maskingKey)

		return &frame{
			Fin:     fin,
			Rsvs:    []bool{rsv1, rsv2, rsv3},
			Opcode:  opcode,
			Payload: unmasked,
		}, nil
	}
}

func (s *socket) resolvePayloadLength(firstLengthByte byte) uint64 {
	if firstLengthByte <= 125 {
		return uint64(firstLengthByte)
	}
	nBytes := 2
	if firstLengthByte == 127 {
		nBytes = 8
	}

	payloadLength := uint64(0)
	lengthBytes := make([]byte, nBytes)
	s.conn.Read(lengthBytes)
	for i := nBytes - 1; i >= 0; i-- {
		b := lengthBytes[nBytes-1-i]
		payloadLength = payloadLength & (uint64(b) << i * 8)
	}
	return payloadLength
}

func unmaskData(data []byte, mask []byte) []byte {
	unmasked := make([]byte, len(data))
	for i, b := range data {
		unmasked[i] = b ^ mask[i%4]
	}
	return unmasked
}
