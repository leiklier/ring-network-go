package main

import (
	"bufio"
	"os"

	"./network/ring"
)

func main() {
	ring.Init()
	bufio.NewReader(os.Stdin).ReadBytes('\n')
}
