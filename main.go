package main

import (
	"bikappa/ws"
	"fmt"
	"io"
	"strings"
)

func main() {
	sv := ws.NewServer()

	sv.Listen("localhost:8080", func(err error, s ws.Socket) {

		if err != nil {
			panic(err)
		}

		s.OnMessage(func(msgType byte, r io.Reader) {
			buf := new(strings.Builder)
			io.Copy(buf, r)
			// check errors
			fmt.Println("received", buf.String())
		})
	})
}
