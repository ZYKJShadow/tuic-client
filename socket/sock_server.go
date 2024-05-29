package socket

import (
	"errors"
	"github.com/ZYKJShadow/tuic-protocol-go/address"
	"github.com/ZYKJShadow/tuic-protocol-go/protocol"
	"github.com/sirupsen/logrus"
	"github.com/txthinking/socks5"
	"net"
	"sync/atomic"
	"tuic-client/client"
	"tuic-client/config"
)

type SockServer struct {
	*socks5.Server
	*config.SocksConfig
	*client.TUICClient

	assocID atomic.Uint32
}

func NewSockServer(config *config.SocksConfig, c *client.TUICClient) (*SockServer, error) {
	server, err := socks5.NewClassicServer(config.Server, config.Ip, config.Username, config.Password, 10, 10)
	if err != nil {
		logrus.Errorf("[socks5] create server failed, err: %v", err)
		return nil, err
	}

	return &SockServer{Server: server, SocksConfig: config, TUICClient: c}, nil
}

func (s *SockServer) Start() error {
	logrus.Infof("[socks5] server started, listening on %s", s.Addr)

	err := s.ListenAndServe(s)
	if err != nil {
		logrus.Errorf("[socks5] server start failed, err: %v", err)
		return err
	}

	return nil
}

func (s *SockServer) TCPHandle(_ *socks5.Server, c *net.TCPConn, r *socks5.Request) error {
	switch r.Cmd {
	case socks5.CmdUDP:
		return s.onHandleAssociate(c, r)
	case socks5.CmdConnect:
		return s.onHandleConnect(c, r)
	case socks5.CmdBind:
	default:
		return socks5.ErrUnsupportCmd
	}

	return nil
}

func (s *SockServer) UDPHandle(_ *socks5.Server, addr *net.UDPAddr, d *socks5.Datagram) error {
	logrus.Infof("[socks5] handle udp, addr: %s, data: %s target:%s", addr, d.Data, d.Address())

	targetAddr, err := net.ResolveUDPAddr(protocol.NetworkUdp, d.Address())
	if err != nil {
		logrus.Errorf("[socks5] handle udp failed, err: %v", err)
		return err
	}

	s.TUICClient.OnHandleUdpConnect(uint16(s.assocID.Add(1)), d.Data, &address.SocketAddress{Addr: net.TCPAddr{
		IP:   targetAddr.IP,
		Port: targetAddr.Port,
	}})

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

	err = s.TUICClient.OnHandleTcpConnect(c, &address.SocketAddress{Addr: *addr})
	if err != nil {
		return err
	}

	return nil
}

func (s *SockServer) onHandleAssociate(c *net.TCPConn, r *socks5.Request) error {
	_, err := r.UDP(c, s.ServerAddr)
	if err != nil {
		return err
	}

	return nil
}
