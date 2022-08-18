package main

import (
	"bikappa/ws"
)

func main() {
	sv := ws.NewEchoServer()

	sv.Listen(":9000")
}
