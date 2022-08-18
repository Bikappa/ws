package main

import (
	"bikappa/ws"
	"bytes"
	"encoding/hex"
	"fmt"
	"io"
	"strings"
)

func main() {
	sv := ws.NewServer()

	sv.Listen("localhost:9000", func(err error, s ws.Socket) {

		if err != nil {
			panic(err)
		}

		msgType := byte(0x00)
		var fragments [][]byte

		s.OnMessage(func(fragmentType byte, r io.Reader, fin bool) {
			buf := new(strings.Builder)
			io.Copy(buf, r)
			// check errors

			fragmentData := []byte(buf.String())
			fmt.Println("received", hex.Dump(fragmentData), fin)
			fragments = append(fragments, fragmentData)
			fmt.Print(fragments)
			if len(fragments) == 1 {
				msgType = fragmentType
			}

			if fin == true {
				for i, data := range fragments {
					fragmentOpcode := msgType
					if i != 0 {
						fragmentOpcode = ws.OPCODE_CONTINUATION
					}
					s.SendMessage(fragmentOpcode, bytes.NewReader(data), i == len(fragments)-1)
				}
				fragments = nil
			}
		})
	})
}
