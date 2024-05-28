package channel

import (
	"github.com/ZYKJShadow/tuic-protocol-go/address"
	"net"
	"sync"
)

type Packet struct {
	RemoteAddr address.Address
	AssocID    uint16
	Data       []byte
	TcpConn    *net.TCPConn
	UdpConn    *net.UDPConn
	ConnID     uint16
}

type channelManager struct {
	udpChannel         chan *Packet
	tcpChannel         chan *Packet
	tcpCompleteChannel map[uint16]chan struct{}
	udpCompleteChannel map[uint16]chan struct{}
	mu                 sync.RWMutex
}

var manager = &channelManager{
	udpChannel:         make(chan *Packet, 100),
	tcpChannel:         make(chan *Packet, 100),
	tcpCompleteChannel: make(map[uint16]chan struct{}, 100),
	udpCompleteChannel: make(map[uint16]chan struct{}, 100),
}

func SetUdpChanPacket(n *Packet) {
	manager.mu.Lock()
	defer manager.mu.Unlock()

	if _, ok := manager.udpCompleteChannel[n.AssocID]; !ok {
		manager.udpCompleteChannel[n.AssocID] = make(chan struct{})
	}

	manager.udpChannel <- n
}

func SetTcpChanPacket(n *Packet) {
	manager.mu.Lock()
	defer manager.mu.Unlock()

	if _, ok := manager.tcpCompleteChannel[n.ConnID]; !ok {
		manager.tcpCompleteChannel[n.ConnID] = make(chan struct{})
	}

	manager.tcpChannel <- n
}

func ReadUdpChanPacket() <-chan *Packet {
	return manager.udpChannel
}

func ReadTcpChanPacket() <-chan *Packet {
	return manager.tcpChannel
}

func SetTcpComplete(connID uint16) {
	manager.mu.RLock()
	defer manager.mu.RUnlock()

	manager.tcpCompleteChannel[connID] <- struct{}{}
}

func ReadTcpComplete(connID uint16) <-chan struct{} {
	manager.mu.RLock()
	defer manager.mu.RUnlock()

	return manager.tcpCompleteChannel[connID]
}

func SetUdpComplete(assocID uint16) {
	manager.mu.RLock()
	defer manager.mu.RUnlock()

	manager.udpCompleteChannel[assocID] <- struct{}{}
}

func ReadUdpComplete(assocID uint16) <-chan struct{} {
	manager.mu.RLock()
	defer manager.mu.RUnlock()

	return manager.udpCompleteChannel[assocID]
}
