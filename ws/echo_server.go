package ws

import (
	"bytes"
	"encoding/hex"
	"fmt"
)

type EchoServer struct {
	srv Server
}

func (e *EchoServer) Listen(url string) {
	e.srv.Listen(url, func(err error, s Socket) {

		if err != nil {
			panic(err)
		}

		// for the echo server testing purposes we want to read frame by frame
		// which may not be available in the public interface
		is, ok := s.(*socket)
		if !ok {
			panic("Unrecognized socket")
		}

		msgType := byte(0x00)
		var fragments [][]byte

		is.OnFrame(func(fragmentType byte, payload []byte, fin bool) {

			fmt.Println("Received frame")
			fmt.Println("Fin:", fin)
			fmt.Println("FragmentType:", fragmentType)
			fmt.Println("length:", len(payload))
			fmt.Println(hex.Dump(payload))

			fragments = append(fragments, payload)
			if len(fragments) == 1 {
				msgType = fragmentType
			}

			if fin {
				for i, data := range fragments {
					fragmentOpcode := msgType
					if i != 0 {
						fragmentOpcode = OPCODE_CONTINUATION
					}
					is.sendFrame(fragmentOpcode, bytes.NewReader(data), i == len(fragments)-1)
				}
				fragments = nil
			}
		})
	})
}

func NewEchoServer() *EchoServer {
	return &EchoServer{
		srv: NewServer(),
	}
}
