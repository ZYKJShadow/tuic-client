package socket

import (
	"errors"
	"github.com/ZYKJShadow/tuic-protocol-go/address"
	"github.com/ZYKJShadow/tuic-protocol-go/protocol"
	"github.com/sirupsen/logrus"
	"github.com/txthinking/socks5"
	"net"
	"sync"
	"sync/atomic"
	"tuic-client/channel"
	"tuic-client/config"
)

type SockServer struct {
	*socks5.Server
	NextAssocID atomic.Uint32
	DualStack   bool
	*config.SocksConfig
	sync.Mutex
}

func NewSockServer(config *config.SocksConfig) (*SockServer, error) {
	server, err := socks5.NewClassicServer(config.Server, config.Ip, config.Username, config.Password, 10, 10)
	if err != nil {
		logrus.Errorf("[socks5] create server failed, err: %v", err)
		return nil, err
	}

	return &SockServer{Server: server, SocksConfig: config}, nil
}

func (s *SockServer) Start() error {
	err := s.ListenAndServe(s)
	if err != nil {
		logrus.Errorf("[socks5] server start failed, err: %v", err)
		return err
	}

	logrus.Infof("[socks5] server started, listening on %s", s.Addr)
	return nil
}

func (s *SockServer) TCPHandle(server *socks5.Server, c *net.TCPConn, r *socks5.Request) error {
	switch r.Cmd {
	case socks5.CmdUDP:
		return s.onHandleAssociate(server, c, r)
	case socks5.CmdConnect:
		return s.onHandleConnect(c, r)
	case socks5.CmdBind:
	default:
		return socks5.ErrUnsupportCmd
	}

	return nil
}

func (s *SockServer) UDPHandle(server *socks5.Server, addr *net.UDPAddr, d *socks5.Datagram) error {
	return nil
}

func (s *SockServer) onHandleConnect(c *net.TCPConn, r *socks5.Request) error {
	addr, err := net.ResolveTCPAddr(protocol.NetworkTcp, r.Address())
	if err != nil {
		logrus.Errorf("[socks5] handle connect failed, err: %v", err)
		return err
	}

	if addr == nil {
		return errors.New("resolve tcp addr failed")
	}

	channel.SetTcpChanPacket(&channel.Packet{
		Addr: &address.SocketAddress{Addr: *addr},
		Conn: c,
	})

	<-channel.ReadTcpComplete()

	return nil
}

func (s *SockServer) onHandleAssociate(server *socks5.Server, c *net.TCPConn, r *socks5.Request) error {
	id := s.NextAssocID.Add(1)

	targetAddr, err := r.UDP(c, server.ServerAddr)
	if err != nil {
		logrus.Errorf("[socks5] handle associate failed, err: %v", err)
		return err
	}

	protocolAddr := s.getProtocolAddr(targetAddr)

	go func() {

		defer func() {
			err := c.Close()
			if err != nil {
				logrus.Errorf("[socks5] handle associate failed, err: %v", err)
				return
			}
		}()

		var n int
		for {
			b := make([]byte, s.MaxPacketSize)
			n, err = c.Read(b)
			if err != nil {
				logrus.Errorf("[socks5] handle associate failed, err: %v", err)
				break
			}

			d, err := socks5.NewDatagramFromBytes(b[:n])
			if err != nil {
				logrus.Errorf("[socks5] handle associate failed, err: %v", err)
				continue
			}

			channel.SetUdpChanPacket(&channel.Packet{
				Addr:    protocolAddr,
				Data:    d.Data,
				AssocID: uint16(id),
			})
		}
	}()

	return nil
}

func (s *SockServer) getProtocolAddr(targetAddr net.Addr) address.Address {
	switch addr := targetAddr.(type) {
	case *net.UDPAddr:
		return &address.SocketAddress{
			Addr: net.TCPAddr{IP: addr.IP, Port: addr.Port},
		}
	case *net.TCPAddr:
		return &address.SocketAddress{
			Addr: net.TCPAddr{
				IP: addr.IP,
			},
		}
	}
	return &address.NoneAddress{}
}
