package ws

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"io"
)

type EchoServer struct {
	srv Server
}

func (e *EchoServer) Listen(url string) {
	e.srv.Listen(url, func(err error, s Socket) {

		if err != nil {
			panic(err)
		}

		msgType := byte(0x00)
		var fragments [][]byte

		s.OnFrame(func(fragmentType byte, r io.Reader, fin bool) {
			buf := new(bytes.Buffer)
			io.Copy(buf, r)

			fmt.Println("Received frame")
			fmt.Println("Fin:", fin)
			fmt.Println("FragmentType:", fragmentType)
			fmt.Println(hex.Dump(buf.Bytes()))

			fragments = append(fragments, buf.Bytes())
			if len(fragments) == 1 {
				msgType = fragmentType
			}

			if fin {
				for i, data := range fragments {
					fragmentOpcode := msgType
					if i != 0 {
						fragmentOpcode = OPCODE_CONTINUATION
					}
					s.SendMessage(fragmentOpcode, bytes.NewReader(data), i == len(fragments)-1)
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
