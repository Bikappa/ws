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

func createTestSocket() (Socket, io.ReadWriteCloser) {
	clientToServer := bytes.NewBuffer([]byte{})
	serverToClient := bytes.NewBuffer([]byte{})
	socket := &socket{
		rwc: &MockedReadWriteCloser{
			clientToServer,
			serverToClient,
			false,
		},
	}

	return socket, &MockedReadWriteCloser{
		serverToClient,
		clientToServer,
		false,
	}
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
