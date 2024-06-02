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

	_, err = conn.Write([]byte("如果那两个字没有颤抖，我不会发现我难受，怎么说出口？也不过是分手，如果对于明天没有要求，牵牵手就像旅游，成千上万个门口，总有一个人要先走。怀抱既然不能逗留，何不在离开的时候，一边享受，一边泪流。十年之前，我不认识你，你不属于我，我们还是一样，站在一个陌生人左右，走过渐渐熟悉的街头，十年之后，我们是朋友，还可以问候，只是那种温柔，再也找不到拥抱的理由。"))
	if err != nil {
		return err
	}

	return nil
}
