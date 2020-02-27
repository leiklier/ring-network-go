package ring

import (
	"fmt"
	"time"

	"../bcast"
	"../peers"
)

// Number of JOIN messages to send before
// declaring that no network exists:
const gJoinMaxRetryAttempts = 5
const gJoinMessage = "JOIN"
const gJoinAckMessage = "JOIN_ACK"

var gIsInitialized = false

type UdpMessage struct {
	SenderIP string
	Data     []byte
}

var gUdpMessageChannel = make(chan UdpMessage, 100)

func Init() {
	if gIsInitialized {
		return
	}
	gIsInitialized = true
	go udpMessageReceiver()

	didJoinExistingNetwork := tryJoiningExistingNetwork()
	fmt.Printf("Existing network? %d", didJoinExistingNetwork)
	if didJoinExistingNetwork {
		return
	}

	createNewNetwork()

}

func createNewNetwork() {
	shouldShutDownChannel := make(chan bool, 10)
	go listenJoin(shouldShutDownChannel)
}

func ringMaintainer() {
	nextNode := peers.GetRelativeTo(peers.Self, 1)
	for {
		peersChange := peers.PollUpdate()

		if peersChange.Event == peers.Removed && peersChange.Peer == nextNode {
			// The peer in which we were connected to disconnected,
			// so connect to the node after that and broadcast the change:
		}
	}
}

func listenJoin(shouldShutDownChannel chan bool) {
	bcast.StartReceiving()
	defer bcast.StopReceiving()

	for {
		select {
		case joinMessage := <-gUdpMessageChannel:
			ipOfNodeTryingToJoin := joinMessage.SenderIP
			bcast.SendTo(ipOfNodeTryingToJoin, []byte(gJoinAckMessage))
			peers.AddTail(ipOfNodeTryingToJoin)
			break

		case <-shouldShutDownChannel:
			return
		}
	}
}

func tryJoiningExistingNetwork() bool {
	isExistingNetwork := false

	bcast.StartReceiving()
	defer bcast.StopReceiving()

	joinMessageInterval := 200 * time.Millisecond
	shouldSendJoinMessageTicker := time.NewTicker(joinMessageInterval)

	for attemptNo := 0; attemptNo < gJoinMaxRetryAttempts; attemptNo++ {
		select {
		case <-shouldSendJoinMessageTicker.C:
			fmt.Printf("Sending JOIN message...\n")
			bcast.SendTo(bcast.BroadcastIP, []byte(gJoinMessage))
			break
		case <-gUdpMessageChannel:
			isExistingNetwork = true
		}
	}

	if !isExistingNetwork {
		return false
	}

	return true
}

func udpMessageReceiver() {
	for {
		senderIP, data := bcast.Receive()
		message := UdpMessage{
			SenderIP: senderIP,
			Data:     data,
		}
		gUdpMessageChannel <- message
	}
}
