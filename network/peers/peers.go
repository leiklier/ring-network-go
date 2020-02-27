package peers

import (
	"net"
	"strings"
)

// Enums
const (
	Get = iota
	Replace
	Delete
	Append
	Self
	Head
	Tail
	Added    = "Added"    // Used when a peer was added
	Removed  = "Removed"  // Used when a peer was removed
	Replaced = "Replaced" // Used when all peers are replaced by new
)

type ControlSignal struct {
	Command         int // Get or Append
	Payload         []string
	ResponseChannel chan []string
}

var controlChannel = make(chan ControlSignal, 100)

type ChangeEvent struct {
	Event string // Added or Removed
	Peer  string
}

var changeChannel = make(chan ChangeEvent, 100)

// Local variables
var isInitialized = false
var localIP string

// Set takes an array of IP addresses, DELETES all existing
// peers and adds the new IP addresses instead, in the same
// order as they appear in the IPs array.
func Set(IPs []string) {
	initialize()
	controlSignal := ControlSignal{
		Command: Replace,
		Payload: IPs,
	}
	controlChannel <- controlSignal
}

func BecomeHead() {
	initialize()
	controlSignal := ControlSignal{
		Command: Head,
	}
	controlChannel <- controlSignal
}

// Remove deletes the peer with a certain IP
func Remove(IP string) {
	initialize()
	controlSignal := ControlSignal{
		Command: Delete,
		Payload: []string{IP},
	}
	controlChannel <- controlSignal
}

// AddTail takes an IP address in the form of a string,
// and adds it at the end of the list of peers, thus
// creating a new tail. It returns nothing
func AddTail(IP string) {
	initialize()
	controlSignal := ControlSignal{
		Command: Append,
		Payload: []string{IP},
	}
	controlChannel <- controlSignal
}

// PollUpdate blocks until a new change has occured in the peers
// when such a change has occured, it returns a ChangeEvent struct
// which has the following format:
// type ChangeEvent struct {
// 		Event string // Added or Removed
//		Peer  string
// }
func PollUpdate() ChangeEvent {
	initialize()
	return <-changeChannel
}

// GetAll returns the array of peers in the correct order
// so the first element is HEAD and the last element is Tail
func GetAll() []string {
	initialize()
	controlSignal := ControlSignal{
		Command:         Get,
		ResponseChannel: make(chan []string),
	}
	controlChannel <- controlSignal
	peers := <-controlSignal.ResponseChannel
	return peers
}

// GetRelativeTo takes either peers.Head, peers.Tail or peers.Self
// as first argument. Then it returns the ip of that peer if offset=0.
// If offset is not 0, then it adds that such that i.e. with role=peers.Self
// and offset=1, it returns the peer AFTER Self, and with offset=-1 it returns
// the peer BEFORE Self.
func GetRelativeTo(role int, offset int) string {
	initialize()
	peers := GetAll()

	var indexOfRole int
	if role == Head {
		indexOfRole = 0
	} else if role == Tail {
		indexOfRole = len(peers) - 1
	} else if role == Self {
		for index, peer := range peers {
			if peer == localIP {
				indexOfRole = index
				break
			}
		}
	}

	indexWithOffset := indexOfRole + offset
	indexWithOffset = indexWithOffset % len(peers)
	if indexWithOffset < 0 {
		indexWithOffset += len(peers)
	}

	return peers[indexWithOffset]
}

func initialize() {
	if isInitialized {
		return
	}
	isInitialized = true
	localIP, _ = getLocalIP()
	go peersServer()
}

func peersServer() {
	peers := make([]string, 1)
	peers[0] = localIP
	for {
		controlSignal := <-controlChannel
		switch controlSignal.Command {
		case Get:
			controlSignal.ResponseChannel <- peers
			break

		case Append:
			peers = append(peers, controlSignal.Payload...)
			for _, newPeer := range controlSignal.Payload {
				changeEvent := ChangeEvent{
					Event: Added,
					Peer:  newPeer,
				}
				changeChannel <- changeEvent
			}
			break

		case Replace:
			peers = controlSignal.Payload
			changeEvent := ChangeEvent{
				Event: Replaced,
			}
			changeChannel <- changeEvent
			break

		case Head:
			var rotation int
			var val string
			var newPeers []string

			for rotation, val = range peers {
				if val == localIP {
					break
				}
			}
			size := len(peers)
			for i := 0; i < rotation; i++ {
				newPeers = peers[1:size]
				newPeers = append(newPeers, peers[0])
				peers = newPeers
			}
			break

		case Delete:
			peerToRemove := controlSignal.Payload[0]
			for i, peer := range peers {
				if peer == peerToRemove {
					copy(peers[i:], peers[i+1:]) // Shift peers[i+1:] left one index.
					peers[len(peers)-1] = ""     // Erase last element (write zero value).
					peers = peers[:len(peers)-1] // Truncate slice.
					changeEvent := ChangeEvent{
						Event: Removed,
						Peer:  peer,
					}
					changeChannel <- changeEvent
					break
				}
			}
		}
	}
}

func IsEqualTo(peersToCompare []string) bool {
	currentPeers := GetAll()

	// If one is nil, the other must also be nil.
	if peersToCompare == nil {
		return false
	}

	if len(peersToCompare) != len(currentPeers) {
		return false
	}

	for i := range peersToCompare {
		if peersToCompare[i] != currentPeers[i] {
			return false
		}
	}

	return true
}

func getLocalIP() (string, error) {
	conn, err := net.DialTCP("tcp4", nil, &net.TCPAddr{IP: []byte{8, 8, 8, 8}, Port: 53})
	if err != nil {
		return "", err
	}
	defer conn.Close()
	return strings.Split(conn.LocalAddr().String(), ":")[0], nil
}
