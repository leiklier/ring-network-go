package messages

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"time"

	"../peers"
	"../receivers"
)

// Enums
const (
	Broadcast = iota
	Ping
	PingAck
)

type Message struct {
	Purpose  string
	Type     int    // Broadcast or Ping or PingAck
	SenderIP string // Only necessary for Broadcast (we need to know where it started...)
	Data     []byte
}

// Variables
var gIsInitialized = false
var gPort = 6970

// Channels
var gServerIPChannel = make(chan string)
var gConnectedToServerChannel = make(chan string)
var gDisconnectedFromServerChannel = make(chan string)

// TODO: Make these channel names more meaningful
var gSendForwardChannel = make(chan Message, 100)
var gSendBackwardChannel = make(chan Message, 100)

func ConnectTo(IP string) error {
	initialize()
	gServerIPChannel <- IP
	select {
	case <-gConnectedToServerChannel:
		return nil
	case <-time.After(2 * time.Second):
		return errors.New("TIMED_OUT")
	}
}

func ServerDisconnected() string {
	return <-gDisconnectedFromServerChannel
}

// Send takes a byte array and sends it
// to the node which it is connected to by ConnectTo
// purpose is used to filter the message on the receiving end
func Send(purpose string, data []byte) {
	initialize()

	localIP := peers.GetRelativeTo(peers.Self, 0)
	message := Message{
		Purpose:  purpose,
		Type:     Broadcast,
		SenderIP: localIP,
		Data:     data,
	}
	gSendForwardChannel <- message
}

func Receive(purpose string) []byte {
	initialize()
	return <-receivers.GetChannel(purpose)
}

func Initialize() {
	initialize()
}

func initialize() {
	if gIsInitialized {
		return
	}
	gIsInitialized = true
	go client()
	go server()
}

func client() {
	serverIP := <-gServerIPChannel
	var shouldDisconnectChannel = make(chan bool, 10)
	go handleOutboundConnection(serverIP, shouldDisconnectChannel)

	// We only want one active client at all times:
	for {
		newServerIp := <-gServerIPChannel
		if newServerIp == serverIP {
			// No need to just reconnect
			continue
		}
		serverIP = newServerIp
		shouldDisconnectChannel <- true
		shouldDisconnectChannel = make(chan bool, 10)
		go handleOutboundConnection(serverIP, shouldDisconnectChannel)
	}
}

func handleOutboundConnection(serverIP string, shouldDisconnectChannel chan bool) {
	conn, err := net.Dial("tcp", fmt.Sprintf("%s:%d", serverIP, gPort))
	if err != nil {
		fmt.Printf("TCP client connect error: %s", err)
		return
	}

	defer func() {
		fmt.Printf("messages: lost connection to server with IP %s\n", serverIP)
		conn.Close()
		if err != nil {
			gDisconnectedFromServerChannel <- serverIP
		}
	}()

	gConnectedToServerChannel <- serverIP

	shouldSendPingTicker := time.NewTicker(2 * time.Second)

	pingAckReceivedChannel := make(chan Message, 100)
	connErrorChannel := make(chan error)

	// Read new messages from conn and send them on pingAckReceivedChannel.
	// Errors are sent back on connErrorChannel.
	go receiveMessages(conn, pingAckReceivedChannel, connErrorChannel)

	// Send messages that are passed to gSendForwardChannel
	// Errors are sent back on connErrorChannel.
	go sendMessages(conn, gSendForwardChannel, connErrorChannel)

	for {
		select {
		case <-shouldSendPingTicker.C:
			// Send a ping message at regular intervals to check that
			// the connection is still alive
			messageToSend := Message{
				Type: Ping,
			}
			gSendForwardChannel <- messageToSend
			select {
			case <-pingAckReceivedChannel:
				// We received a PingAck, so everything works fine
				break
			case <-time.After(1 * time.Second):
				// Cannot retrieve PingAck, so the connection is
				// not working properly

				err = errors.New("ERR_SERVER_DISCONNECTED")
				return
			}
			break

		case <-shouldDisconnectChannel:
			return

		case <-connErrorChannel:
			err = errors.New("ERR_SERVER_DISCONNECTED")
			return
		}

	}
}

func server() {
	// Boot up TCP server
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", gPort))
	if err != nil {
		fmt.Printf("TCP server listener error: %s", err)
	}

	// Listen to incoming connections
	var shouldDisconnectChannel = make(chan bool, 10)
	for {
		// Accept a new connection
		conn, err := listener.Accept()
		if err != nil {
			fmt.Printf("TCP server accept error: %s", err)
			break
		}
		// A new client connected to us, so disconnect to the one
		// already connected because we only accept one connection
		// at all times
		shouldDisconnectChannel <- true
		shouldDisconnectChannel = make(chan bool, 10)
		handleIncomingConnection(conn, shouldDisconnectChannel)

	}
}

func handleIncomingConnection(conn net.Conn, shouldDisconnectChannel chan bool) {
	defer conn.Close()

	messageReceivedChannel := make(chan Message, 100)
	connErrorChannel := make(chan error)

	// Read new messages from conn and send them on pingAckReceivedChannel.
	// Errors are sent back on connErrorChannel.
	go receiveMessages(conn, messageReceivedChannel, connErrorChannel)

	// Send messages that are passed to gSendForwardChannel
	// Errors are sent back on connErrorChannel.
	go sendMessages(conn, gSendBackwardChannel, connErrorChannel)

	for {
		select {
		// We have received a message
		case messageReceived := <-messageReceivedChannel:

			switch messageReceived.Type {
			case Broadcast:
				localIP := peers.GetRelativeTo(peers.Self, 0)

				if messageReceived.SenderIP != localIP {
					// We should forward the message to next node
					receivers.GetChannel(messageReceived.Purpose) <- messageReceived.Data
					gSendForwardChannel <- messageReceived
				}

				break
			case Ping:
				messageToSend := Message{
					Type: PingAck,
				}
				gSendBackwardChannel <- messageToSend
				break
			}
			break

		case <-shouldDisconnectChannel:
			return

		case <-connErrorChannel:
			return
		}

	}
}

func receiveMessages(conn net.Conn, receiveChannel chan Message, errorChannel chan error) {
	for {
		bytesReceived, err := bufio.NewReader(conn).ReadBytes('\n')
		if err != nil {
			errorChannel <- err
			return
		}

		var messageReceived Message
		json.Unmarshal(bytesReceived, &messageReceived)

		receiveChannel <- messageReceived

	}
}

func sendMessages(conn net.Conn, messageToSendChannel chan Message, errorChannel chan error) {
	for {
		messageToSend := <-messageToSendChannel
		serializedMessage, _ := json.Marshal(messageToSend)

		_, err := fmt.Fprintf(conn, string(serializedMessage)+"\n\000")

		if err != nil {
			// We need to retransmit the message, to pass it back to the channel.
			// However, the connection is not working so disconnect.
			messageToSendChannel <- messageToSend
			errorChannel <- err
			return
		}
	}
}
