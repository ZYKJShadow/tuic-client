package socket

import (
	"github.com/ZYKJShadow/tuic-protocol-go/protocol"
	"github.com/txthinking/socks5"
	"tuic-client/config"
)

type SockClient struct {
	*socks5.Client
}

func NewSockClient(config *config.SocksConfig) (*SockClient, error) {
	client, err := socks5.NewClient(config.Server, config.Username, config.Password, 10, 10)
	if err != nil {
		return nil, err
	}

	return &SockClient{client}, nil
}

func (s *SockClient) Start() error {
	conn, err := s.DialWithLocalAddr(protocol.NetworkUdp, "", "127.0.0.1:6666", nil)
	if err != nil {
		return err
	}

	_, err = conn.Write([]byte("hello"))
	if err != nil {
		return err
	}

	return nil
}
