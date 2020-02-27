package ring

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"../bcast"
	"../messages"
	"../peers"
)

// Number of JOIN messages to send before
// declaring that no network exists:
const gJoinMaxRetryAttempts = 5
const gJoinMessage = "JOIN"
const gJoinAckMessage = "JOIN_ACK"

var gJoinAcceptorIsRunning = false
var gJoinAcceptorIsRunningMutex sync.Mutex
var gShouldStopJoinAcceptorChannel = make(chan bool)

const (
	NewPeersList = "_NewPeersList"
)

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
	messages.Initialize()

	go udpMessageReceiver()

	didJoinExistingNetwork := tryJoiningExistingNetwork()
	fmt.Printf("Existing network? %d", didJoinExistingNetwork)
	if didJoinExistingNetwork {
		return
	}

	createNewNetwork()

}

func createNewNetwork() {
	go newPeersReceiver()
	go ringMaintainer()
}

func ringMaintainer() {

	for {
		headPeer := peers.GetRelativeTo(peers.Head, 0)
		selfPeer := peers.GetRelativeTo(peers.Self, 0)
		nextPeer := peers.GetRelativeTo(peers.Self, 1)

		if nextPeer != selfPeer {
			messages.ConnectTo(nextPeer)
		}

		if selfPeer == headPeer {
			startJoinAcceptor()
		} else {
			stopJoinAcceptor()
		}

		peers.PollDidUpdate()
		fmt.Println(peers.GetAll())
	}
}

func newPeersReceiver() {
	for {
		var newPeersList []string

		serializedNewPeersList := messages.Receive(NewPeersList)
		json.Unmarshal(serializedNewPeersList, newPeersList)

		peers.Set(newPeersList)
	}
}

func nextNodeWatcher() {
	for {
		nextPeerIP := messages.ServerDisconnected()
		fmt.Println("did disconnect")
		peers.Remove(nextPeerIP)

		newNextPeer := peers.GetRelativeTo(peers.Self, 1)
		// We have to reconnect here in order for the
		// message to be sent:
		messages.ConnectTo(newNextPeer)
		serializedPeersList, _ := json.Marshal(peers.GetAll())
		messages.Send(NewPeersList, serializedPeersList)

	}
}

func startJoinAcceptor() {
	gJoinAcceptorIsRunningMutex.Lock()
	if !gJoinAcceptorIsRunning {
		go joinAcceptor()
	}
	gJoinAcceptorIsRunningMutex.Unlock()
}

func stopJoinAcceptor() {
	gJoinAcceptorIsRunningMutex.Lock()
	if gJoinAcceptorIsRunning {
		gShouldStopJoinAcceptorChannel <- true
	}
	gJoinAcceptorIsRunningMutex.Unlock()
}

func joinAcceptor() {
	gJoinAcceptorIsRunningMutex.Lock()
	gJoinAcceptorIsRunning = true
	gJoinAcceptorIsRunningMutex.Unlock()

	bcast.StartReceiving()
	defer func() {
		gJoinAcceptorIsRunningMutex.Lock()
		gJoinAcceptorIsRunning = false
		gJoinAcceptorIsRunningMutex.Unlock()

		bcast.StopReceiving()
	}()

	for {
		select {
		case joinMessage := <-gUdpMessageChannel:
			ipOfNodeTryingToJoin := joinMessage.SenderIP
			bcast.SendTo(ipOfNodeTryingToJoin, []byte(gJoinAckMessage))
			peers.AddTail(ipOfNodeTryingToJoin)

			// Let all nodes get the new list of peers s.t.
			// the ring will expand:
			serializedPeersList, _ := json.Marshal(peers.GetAll())
			messages.Send(NewPeersList, serializedPeersList)
			break

		case <-gShouldStopJoinAcceptorChannel:
			return
		}
	}
}

func tryJoiningExistingNetwork() bool {
	isExistingNetwork := false

	bcast.StartReceiving()
	//defer bcast.StopReceiving()

	joinMessageInterval := 250 * time.Millisecond
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
		go newPeersReceiver()
		go ringMaintainer()
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
