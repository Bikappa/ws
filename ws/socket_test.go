package ws

import (
	"bytes"
	"io"
	"testing"
)

type MockedReadWriteCloser struct {
	io.ReadWriter
	closeCalled bool
}

func (mrwc *MockedReadWriteCloser) Close() error {
	mrwc.closeCalled = true
	return nil
}

func createTestSocket() (Socket, io.ReadWriteCloser) {
	socket := &socket{
		rwc: &MockedReadWriteCloser{
			bytes.NewBuffer([]byte{}),
			false,
		},
	}

	return socket, socket.rwc
}

func TestPingReply(t *testing.T) {
	_, rwc := createTestSocket()

	payloadReader := bytes.NewReader([]byte("hello"))
	rwc.Write(createMessageFrame(payloadReader, uint64(payloadReader.Len()), OPCODE_PING, true))
	frame, err := decodeFrame(decodeFrameSettings{
		reader:                          rwc,
		danglingUTF8Bytes:               []byte{},
		expectedContinuationMessageType: 0,
		expectedMask:                    false,
	})

	if err != nil {
		t.Error("Unexpected error while decoding frame", err)
	}

	if frame.Opcode != OPCODE_PONG {
		t.Error("Replied with non-pong opcode", frame.Opcode)
	}

	if string(frame.Payload) != "hello" {
		t.Error("Replied with wrong payload", string(frame.Payload))
	}
}
