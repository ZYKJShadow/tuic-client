package client

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"github.com/ZYKJShadow/tuic-protocol-go/address"
	"github.com/ZYKJShadow/tuic-protocol-go/options"
	"github.com/ZYKJShadow/tuic-protocol-go/protocol"
	"github.com/ZYKJShadow/tuic-protocol-go/utils"
	"github.com/quic-go/quic-go"
	"github.com/sirupsen/logrus"
	"io"
	"net"
	"time"
	"tuic-client/channel"
	"tuic-client/config"
)

type TUICClient struct {
	ctx  context.Context
	conn quic.Connection
	*config.ClientConfig
}

func NewTUICClient(ctx context.Context, cfg *config.ClientConfig) (*TUICClient, error) {
	return &TUICClient{
		ClientConfig: cfg,
		ctx:          ctx,
	}, nil
}

func (c *TUICClient) Start() error {
	err := c.dial()
	if err != nil {
		return err
	}

	go c.heartbeat()
	go c.listenUdpConn()
	go c.listenTcpConn()

	return nil
}

func (c *TUICClient) dial() error {
	cfg := c.ClientConfig
	certs, err := utils.LoadCerts(cfg.CertPath)
	if err != nil {
		logrus.Errorf("load certs failed: %v", err)
		return err
	}

	certPool, err := utils.AddSelfSignedCertToClientPool(cfg.CertPath)
	if err != nil {
		logrus.Errorf("add self signed cert to client pool failed: %v", err)
		return err
	}

	tlsConfig := &tls.Config{
		NextProtos:         cfg.ALPN,
		InsecureSkipVerify: true,
		CipherSuites: []uint16{
			tls.TLS_AES_128_GCM_SHA256,
			tls.TLS_AES_256_GCM_SHA384,
			tls.TLS_CHACHA20_POLY1305_SHA256,
		},
		CurvePreferences: []tls.CurveID{
			tls.X25519,
			tls.CurveP256,
			tls.CurveP384,
		},
		ClientAuth: tls.NoClientCert,
		MinVersion: tls.VersionTLS13,
		RootCAs:    x509.NewCertPool(),
	}

	if certs != nil {
		tlsConfig.Certificates = []tls.Certificate{
			{
				Certificate: certs,
			},
		}
		tlsConfig.RootCAs = certPool
		tlsConfig.InsecureSkipVerify = false
	}

	//goland:noinspection SpellCheckingInspection
	quicConfig := &quic.Config{
		Versions:                       []quic.Version{quic.Version2},
		HandshakeIdleTimeout:           3 * time.Second,
		MaxIdleTimeout:                 10 * time.Second,
		Allow0RTT:                      cfg.ZeroRTTHandshake,
		KeepAlivePeriod:                time.Second * 3,
		EnableDatagrams:                true,
		InitialStreamReceiveWindow:     8 * 1024 * 1024 * 2,
		InitialConnectionReceiveWindow: 8 * 1024 * 1024 * 2,
		MaxIncomingStreams:             cfg.MaxStreamCount,
		MaxIncomingUniStreams:          cfg.MaxStreamCount,
	}

	var conn quic.Connection
	if cfg.ZeroRTTHandshake {
		conn, err = quic.DialAddrEarly(c.ctx, cfg.Server, tlsConfig, quicConfig)
		if err != nil {
			logrus.Errorf("dial addr:%s failed: %v", cfg.Server, err)
			conn, err = quic.DialAddr(c.ctx, cfg.Server, tlsConfig, quicConfig)
		}
	} else {
		conn, err = quic.DialAddr(c.ctx, cfg.Server, tlsConfig, quicConfig)
	}

	if err != nil {
		logrus.Errorf("dial addr:%s failed: %v", cfg.Server, err)
		return err
	}

	logrus.Infof("dial addr:%s success", cfg.Server)
	c.conn = conn

	err = c.authenticate()
	if err != nil {
		return err
	}

	return nil
}

func (c *TUICClient) heartbeat() {
	for {
		time.Sleep(time.Second * 3)
		cmd := protocol.Command{
			Version: protocol.VersionMajor,
			Type:    protocol.CmdHeartbeat,
		}

		if !c.isConnAlive() {
			err := c.dial()
			if err != nil {
				logrus.Errorf("dial addr:%s failed: %v", c.Server, err)
				continue
			}
		}

		b, err := cmd.Marshal()
		if err != nil {
			logrus.Errorf("marshal heartbeat command failed: %v", err)
			continue
		}

		err = c.conn.SendDatagram(b)
		if err != nil {
			logrus.Errorf("send heartbeat failed: %v", err)
			continue
		}
	}
}

func (c *TUICClient) authenticate() error {
	stream, err := c.conn.OpenUniStream()
	if err != nil {
		logrus.Errorf("open uni stream failed: %v", err)
		return err
	}

	defer func(stream quic.SendStream) {
		err = stream.Close()
		if err != nil {
			logrus.Errorf("close stream failed: %v", err)
		}
	}(stream)

	tlsConn := c.conn.ConnectionState().TLS
	token, err := tlsConn.ExportKeyingMaterial(c.UUID[:16], []byte(c.Password), 32)
	if err != nil {
		logrus.Errorf("export keying material failed: %v", err)
		return err
	}

	cmd := protocol.Command{
		Version: protocol.VersionMajor,
		Type:    protocol.CmdAuthenticate,
		Options: &options.AuthenticateOptions{
			UUID:  []byte(c.UUID),
			Token: token,
		},
	}

	err = c.sendCommand(stream, cmd)
	if err != nil {
		return err
	}

	return nil
}

func (c *TUICClient) listenTcpConn() {
	for {
		select {
		case packet, ok := <-channel.ReadTcpChanPacket():
			if !ok {
				return
			}

			if !c.isConnAlive() {
				err := c.dial()
				if err != nil {
					logrus.Errorf("dial failed: %v", err)
					continue
				}
			}

			go func(packet *channel.Packet) {
				defer channel.SetTcpComplete()
				err := c.onHandleTcpConnect(packet)
				if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, net.ErrClosed) {
					var streamErr *quic.StreamError
					if errors.As(err, &streamErr) && streamErr.ErrorCode == quic.StreamErrorCode(0) {
						return
					}

					logrus.Errorf("handle tcp connect failed: %v", err)
				}
			}(packet)
		}
	}
}

func (c *TUICClient) isConnAlive() bool {
	select {
	case <-c.conn.Context().Done():
		return false
	default:
		return true
	}
}

func (c *TUICClient) listenUdpConn() {
	for {
		select {
		case packet, ok := <-channel.ReadUdpChanPacket():
			if !ok {
				logrus.Infof("channel closed")
				return
			}

			if !c.isConnAlive() {
				err := c.dial()
				if err != nil {
					logrus.Errorf("dial failed: %v", err)
					continue
				}
			}

			opts := &options.PacketOptions{
				AssocID:   packet.AssocID,
				FragTotal: 1,
				FragID:    0,
				Size:      0,
				Addr:      packet.Addr,
			}

			opts.CalFragTotal(packet.Data, 2048)
			if opts.FragTotal > 1 {
				// 分片发送
				fragSize := len(packet.Data) / int(opts.FragTotal)
				for i := 0; i < int(opts.FragTotal); i++ {
					opts.FragID = uint8(i)
					start := i * fragSize
					end := start + fragSize
					if i == int(opts.FragTotal)-1 {
						end = len(packet.Data)
					}
					opts.Size = uint16(end - start)

					fragData := packet.Data[start:end]
					cmd := protocol.Command{
						Version: protocol.VersionMajor,
						Type:    protocol.CmdPacket,
						Options: opts,
					}

					cmdBytes, err := cmd.Marshal()
					if err != nil {
						logrus.Errorf("marshal packet failed: %v", err)
						continue
					}

					cmdBytes = append(cmdBytes, fragData...)

					switch c.UDPRelayMode {
					case protocol.UdpRelayModeQuic:
						err = c.sendFragments(cmdBytes)
					case protocol.UdpRelayModeNative:
						err = c.conn.SendDatagram(cmdBytes)
					}

					if err != nil {
						logrus.Errorf("send fragment failed: %v", err)
						continue
					}
				}
			} else {
				opts.Size = uint16(len(packet.Data))
				cmd := protocol.Command{
					Version: protocol.VersionMajor,
					Type:    protocol.CmdPacket,
					Options: opts,
				}

				cmdBytes, err := cmd.Marshal()
				if err != nil {
					logrus.Errorf("marshal packet failed: %v", err)
					continue
				}

				cmdBytes = append(cmdBytes, packet.Data...)

				switch c.UDPRelayMode {
				case protocol.UdpRelayModeQuic:
					err = c.sendFragments(cmdBytes)
				case protocol.UdpRelayModeNative:
					err = c.conn.SendDatagram(cmdBytes)
				}

				if err != nil {
					logrus.Errorf("send data failed: %v", err)
					continue
				}
			}
		}
	}
}

func (c *TUICClient) sendFragments(data []byte) error {
	stream, err := c.conn.OpenUniStream()
	if err != nil {
		return fmt.Errorf("open stream failed: %v", err)
	}

	defer func(stream quic.SendStream) {
		err = stream.Close()
		if err != nil {
			logrus.Errorf("close stream failed: %v", err)
		}
	}(stream)

	_, err = io.Copy(stream, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("write stream failed: %v", err)
	}

	return nil
}

func (c *TUICClient) onHandleTcpConnect(packet *channel.Packet) error {
	defer func() {
		_ = packet.Conn.Close()
	}()

	opts := &options.ConnectOptions{
		Addr: packet.Addr,
	}

	ctx, cancel := context.WithDeadline(c.ctx, time.Now().Add(time.Second*3))
	defer cancel()

	stream, err := c.conn.OpenStreamSync(ctx)
	if err != nil {
		logrus.Errorf("open stream failed: %v", err)
		return err
	}

	_ = stream.SetDeadline(time.Now().Add(time.Second * 10))

	defer c.onCloseStream(stream)

	cmd := protocol.Command{
		Version: protocol.VersionMajor,
		Type:    protocol.CmdConnect,
		Options: opts,
	}

	err = c.sendCommand(stream, cmd)
	if err != nil {
		logrus.Errorf("send command failed: %v", err)
		return err
	}

	conn := packet.Conn

	errCh := make(chan error, 2)
	go func() {
		_, err := io.Copy(conn, stream)
		errCh <- err
	}()

	go func() {
		_, err := io.Copy(stream, conn)
		errCh <- err
	}()

	return <-errCh
}

func (c *TUICClient) sendCommand(stream quic.SendStream, cmd protocol.Command) error {
	// 发送Command
	b, err := cmd.Marshal()
	if err != nil {
		return err
	}

	// 发送Command数据
	_, err = stream.Write(b)
	if err != nil {
		return err
	}

	return nil
}

func (c *TUICClient) onHandlePacket(stream io.Reader) error {
	// 反序列化options,处理UDP数据包,必要时建立新的UDP会话
	optBytes := make([]byte, protocol.PacketLen)
	_, err := io.ReadFull(stream, optBytes)
	if err != nil {
		logrus.Errorf("onHandlePacket io.ReadFull err:%v", err)
		return err
	}

	if len(optBytes) < protocol.PacketLen {
		return errors.New("invalid packet")
	}

	var opts options.PacketOptions
	err = opts.Unmarshal(optBytes)
	if err != nil {
		logrus.Errorf("options.PacketOptions.Unmarshal err:%v", err)
		return err
	}

	protocolAddr, err := address.UnMarshalAddr(stream)
	if err != nil {
		logrus.Errorf("address.UnMarshalAddr err:%v", err)
		return err
	}

	opts.Addr = protocolAddr

	// 计算实际数据长度
	data := make([]byte, opts.Size)
	_, err = io.ReadFull(stream, data)
	if err != nil {
		logrus.Errorf("onHandlePacket read real data err:%v", err)
		return err
	}

	addr, err := opts.Addr.ResolveDNS()
	if err != nil {
		logrus.Errorf("[relay] [packet] [%06x] [%06x] failed to resolve dns: %v", opts.AssocID, opts.PacketID, err)
		return err
	}

	if len(addr) == 0 {
		logrus.Errorf("resolve dns empty addr")
		return errors.New("resolve dns empty addr")
	}

	return nil
}

func (c *TUICClient) onCloseStream(stream quic.Stream) {
	stream.CancelWrite(quic.StreamErrorCode(0))
	stream.CancelRead(quic.StreamErrorCode(0))
}
