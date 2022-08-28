package ws

import (
	"bytes"
	"io"
	"testing"
)

type MockedReadWriteCloser struct {
	io.Reader
	io.Writer
	closeCalled bool
}

func (mrwc *MockedReadWriteCloser) Close() error {
	mrwc.closeCalled = true
	return nil
}

func createTestSocket() (*socket, io.ReadWriteCloser) {
	serverR, clientW := io.Pipe()
	clientR, serverW := io.Pipe()
	socket := &socket{
		rwc: &MockedReadWriteCloser{
			serverR,
			serverW,
			false,
		},
	}

	return socket, &MockedReadWriteCloser{
		clientR,
		clientW,
		false,
	}
}

func TestPingReply(t *testing.T) {
	s, rwc := createTestSocket()

	go s.readLoop()

	payloadReader := bytes.NewReader([]byte("hello"))
	rwc.Write(encodeFrame(
		FrameEncodeOptions{
			r:             payloadReader,
			payloadLength: uint64(payloadReader.Len()),
			opCode:        OPCODE_PING,
			fin:           true,
			mask:          true,
		}))

	frame, err := decodeFrame(decodeFrameSettings{
		reader:                          rwc,
		danglingUTF8Bytes:               []byte{},
		expectedContinuationMessageType: 0,
		expectedMask:                    false,
	})

	if err != nil {
		t.Error("Unexpected error while decoding frame", err)
		return
	}

	if frame.Opcode != OPCODE_PONG {
		t.Error("Replied with non-pong opcode", frame.Opcode)
		return
	}

	if string(frame.Payload) != "hello" {
		t.Error("Replied with wrong payload", string(frame.Payload))
		return
	}
}
