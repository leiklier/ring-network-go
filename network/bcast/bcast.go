package bcast

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"sync"
	"syscall"

	"../peers"
)

var gIsInitialized = false
var gPort = 6970
var gReceiverIsRunning = false
var gReceiverIsRunningMutex sync.Mutex

const BroadcastIP = "255.255.255.255"

type Message struct {
	SenderIP   string
	ReceiverIP string
	Data       []byte
}

var gReceiveChannel = make(chan Message, 100)
var gSendChannel = make(chan Message, 100)
var gShouldStopReceivingChannel = make(chan bool)

func initialize() {
	if gIsInitialized {
		return
	}
	gIsInitialized = true
	go sender()
}

func SendTo(receiverIP string, data []byte) {
	initialize()
	message := Message{
		ReceiverIP: receiverIP,
		Data:       data,
	}
	gSendChannel <- message
}

func Receive() (string, []byte) {
	initialize()
	StartReceiving()
	message := <-gReceiveChannel
	return message.SenderIP, message.Data
}

func StartReceiving() {
	initialize()
	gReceiverIsRunningMutex.Lock()
	if !gReceiverIsRunning {
		go receiver()
	}
	gReceiverIsRunningMutex.Unlock()
}

func StopReceiving() {
	initialize()
	gReceiverIsRunningMutex.Lock()
	if gReceiverIsRunning {
		gShouldStopReceivingChannel <- true
	}
	gReceiverIsRunningMutex.Unlock()
}

func sender() {
	conn := dialBroadcastUDP(gPort)
	addr, _ := net.ResolveUDPAddr("udp4", fmt.Sprintf("%s:%d", BroadcastIP, gPort))
	defer conn.Close()

	for {
		message := <-gSendChannel
		message.SenderIP = peers.GetRelativeTo(peers.Self, 0)

		serializedMessage, _ := json.Marshal(message)
		conn.WriteTo(serializedMessage, addr)
	}
}

func receiver() {
	gReceiverIsRunningMutex.Lock()
	gReceiverIsRunning = true
	gReceiverIsRunningMutex.Unlock()

	var buffer [1024]byte
	conn := dialBroadcastUDP(gPort)

	defer func() {
		gReceiverIsRunningMutex.Lock()
		gReceiverIsRunning = false
		gReceiverIsRunningMutex.Unlock()

		conn.Close()
	}()

	for {
		select {
		case <-gShouldStopReceivingChannel:
			return

		default:
			localIP := peers.GetRelativeTo(peers.Self, 0)
			var message Message
			nBytes, _, _ := conn.ReadFrom(buffer[0:])
			json.Unmarshal(buffer[:nBytes], &message)

			if message.SenderIP == localIP {
				// We do not want to receive messages from ourself
				continue
			}

			if message.ReceiverIP != localIP && message.ReceiverIP != BroadcastIP {
				// The message was not intended for us
				continue
			}

			gReceiveChannel <- message
		}
	}
}

func dialBroadcastUDP(port int) net.PacketConn {
	s, _ := syscall.Socket(syscall.AF_INET, syscall.SOCK_DGRAM, syscall.IPPROTO_UDP)
	syscall.SetsockoptInt(s, syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1)
	syscall.SetsockoptInt(s, syscall.SOL_SOCKET, syscall.SO_BROADCAST, 1)
	syscall.Bind(s, &syscall.SockaddrInet4{Port: port})

	f := os.NewFile(uintptr(s), "")
	conn, _ := net.FilePacketConn(f)
	f.Close()

	return conn
}
