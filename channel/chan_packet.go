package channel

import (
	"github.com/ZYKJShadow/tuic-protocol-go/address"
	"net"
)

var udpChannel = make(chan *Packet, 100)
var tcpChannel = make(chan *Packet, 100)

var tcpCompleteChannel = make(chan struct{}, 100)

type Packet struct {
	Addr    address.Address
	AssocID uint16
	Data    []byte
	Conn    *net.TCPConn
}

func SetUdpChanPacket(n *Packet) {
	udpChannel <- n
}

func ReadUdpChanPacket() <-chan *Packet {
	return udpChannel
}

func SetTcpChanPacket(n *Packet) {
	tcpChannel <- n
}

func ReadTcpChanPacket() <-chan *Packet {
	return tcpChannel
}

func SetTcpComplete() {
	tcpCompleteChannel <- struct{}{}
}

func ReadTcpComplete() <-chan struct{} {
	return tcpCompleteChannel
}
