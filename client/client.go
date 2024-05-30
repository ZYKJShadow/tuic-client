package client

import (
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
	"strings"
	"sync"
	"time"
	"tuic-client/config"
)

type TUICClient struct {
	ctx  context.Context
	conn quic.Connection
	udp  *net.UDPConn
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
			logrus.Errorf("dial addr:%s early failed: %v", cfg.Server, err)
		}
	}

	if conn == nil {
		conn, err = quic.DialAddr(c.ctx, cfg.Server, tlsConfig, quicConfig)
		if err != nil {
			logrus.Errorf("dial addr:%s failed: %v", cfg.Server, err)
			return err
		}
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

	err = c.onSendCommand(stream, cmd)
	if err != nil {
		return err
	}

	return nil
}

func (c *TUICClient) isConnAlive() bool {
	select {
	case <-c.conn.Context().Done():
		return false
	default:
		return true
	}
}

func (c *TUICClient) OnHandleUdpConnect(udp *net.UDPConn, assocID uint16, data []byte, remoteAddr address.Address) {
	if c.udp == nil {
		c.udp = udp
	}

	opts := &options.PacketOptions{
		AssocID:   assocID,
		FragTotal: 1,
		FragID:    0,
		Size:      0,
		Addr:      remoteAddr,
	}

	opts.CalFragTotal(data, 2048)
	switch {
	case opts.FragTotal > 1:
		c.onRelayFragmentedUdpSend(data, opts)
	default:
		c.onRelayUdpSend(data, opts)
	}
}

func (c *TUICClient) OnHandleTcpConnect(conn *net.TCPConn, remoteAddr address.Address) error {
	opts := &options.ConnectOptions{
		Addr: remoteAddr,
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

	err = c.onSendCommand(stream, cmd)
	if err != nil {
		logrus.Errorf("send command failed: %v", err)
		return err
	}
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		// 从conn读取数据并写入stream
		_, err := io.Copy(stream, conn)
		if err != nil {
			// 取消写入并通知服务器
			switch {
			case errors.Is(err, net.ErrClosed):
				stream.CancelWrite(protocol.NormalClosed)
			default:
				stream.CancelWrite(protocol.ClientCanceled)

				var e *quic.StreamError
				if errors.As(err, &e) && e.ErrorCode == protocol.NormalClosed {
					return
				}

				logrus.Errorf("stream, packet.TcpConn err: %v", err)

			}

			return
		}

		// 数据发送完毕，正常关闭写入端
		_ = stream.Close()
	}()

	go func() {
		defer wg.Done()
		// 从stream读取数据并写入conn
		_, err := io.Copy(conn, stream)
		if err != nil {
			_ = conn.Close()

			var e *quic.StreamError
			if errors.As(err, &e) && e.ErrorCode == protocol.NormalClosed {
				return
			}

			switch {
			case errors.Is(err, net.ErrClosed):
				return
			case strings.Contains(err.Error(), "deadline exceeded"):
				return
			default:
				logrus.Errorf("conn, stream err: %v", err)
			}

		}
	}()

	wg.Wait()

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

func (c *TUICClient) onSendCommand(stream quic.SendStream, cmd protocol.Command) error {
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

func (c *TUICClient) onRelayFragmentedUdpSend(data []byte, opts *options.PacketOptions) {
	fragSize := len(data) / int(opts.FragTotal)
	for i := 0; i < int(opts.FragTotal); i++ {
		opts.FragID = uint8(i)
		start := i * fragSize
		end := start + fragSize
		if i == int(opts.FragTotal)-1 {
			end = len(data)
		}
		fragment := data[start:end]
		c.onRelayUdpSend(fragment, opts)
	}
}

func (c *TUICClient) onRelayUdpSend(fragment []byte, opts *options.PacketOptions) {
	opts.Size = uint16(len(fragment))
	cmd := protocol.Command{
		Version: protocol.VersionMajor,
		Type:    protocol.CmdPacket,
		Options: opts,
	}

	cmdBytes, err := cmd.Marshal()
	if err != nil {
		logrus.Errorf("marshal packet failed: %v", err)
		return
	}

	cmdBytes = append(cmdBytes, fragment...)

	switch c.UDPRelayMode {
	case protocol.UdpRelayModeQuic:
		err = c.sendFragments(cmdBytes)
	case protocol.UdpRelayModeNative:
		err = c.conn.SendDatagram(cmdBytes)
	default:
		logrus.Errorf("UDP relay mode %s not supported", c.UDPRelayMode)
		return
	}

	if err != nil {
		logrus.Errorf("send data failed: %v", err)
	}
}

func (c *TUICClient) sendFragments(data []byte) error {
	stream, err := c.conn.OpenUniStream()
	if err != nil {
		return fmt.Errorf("open stream failed: %v", err)
	}

	defer func() {
		_ = stream.Close()
	}()

	_, err = stream.Write(data)
	if err != nil {
		return fmt.Errorf("write data failed: %v", err)
	}

	return nil
}
