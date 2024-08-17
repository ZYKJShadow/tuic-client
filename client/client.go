package client

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"github.com/ZYKJShadow/tuic-protocol-go/fragment"
	"github.com/ZYKJShadow/tuic-protocol-go/options"
	"github.com/ZYKJShadow/tuic-protocol-go/protocol"
	"github.com/ZYKJShadow/tuic-protocol-go/utils"
	"github.com/quic-go/quic-go"
	"github.com/sirupsen/logrus"
	"io"
	"net"
	"sync"
	"time"
	"tuic-client/config"
)

type TUICClient struct {
	ctx            context.Context
	conn           quic.Connection
	fragmentCache  *fragment.FCache
	socketCacheMap map[uint16]*net.UDPConn

	*config.ClientConfig
	sync.RWMutex
}

func NewTUICClient(ctx context.Context, cfg *config.ClientConfig) (*TUICClient, error) {
	return &TUICClient{
		ClientConfig:   cfg,
		ctx:            ctx,
		fragmentCache:  fragment.NewFCache(),
		socketCacheMap: make(map[uint16]*net.UDPConn),
	}, nil
}

func (c *TUICClient) Start() error {
	err := c.dial()
	if err != nil {
		return err
	}

	go c.heartbeat()
	go c.onReceiveUniStream()
	go c.onReceiveDatagram()

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

	defer func() {
		_ = stream.Close()
	}()

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

func (c *TUICClient) packet(stream io.Reader, opts *options.PacketOptions, mode string) error {
	data := make([]byte, opts.Size)
	_, err := io.ReadFull(stream, data)
	if err != nil {
		logrus.Errorf("Failed to read packet: %v", err)
		return err
	}

	switch mode {
	case protocol.UdpRelayModeQuic:
		return c.onHandleQUICPacket(data, opts)
	case protocol.UdpRelayModeNative:
		return c.onHandlePacket(data, opts)
	default:
		return errors.New("unknown udp relay mode")
	}
}

func (c *TUICClient) onReceiveUniStream() {
	for {
		stream, err := c.conn.AcceptUniStream(context.Background())
		if err != nil {
			logrus.Errorf("Failed to accept uni stream: %v", err)
			continue
		}

		go c.onHandleUniStream(stream)
	}
}

func (c *TUICClient) onReceiveDatagram() {
	for {
		datagram, err := c.conn.ReceiveDatagram(context.Background())
		if err != nil {
			logrus.Errorf("Failed to receive datagram: %v", err)
			continue
		}

		go c.onHandleDatagram(datagram)
	}
}

func (c *TUICClient) onHandleDatagram(datagram []byte) {
	reader := bytes.NewReader(datagram)
	var cmd protocol.Command
	err := cmd.Unmarshal(reader)
	if err != nil {
		logrus.Errorf("Failed to read command: %v", err)
		return
	}

	switch cmd.Type {
	case protocol.CmdPacket:
		err = c.packet(reader, cmd.Options.(*options.PacketOptions), protocol.UdpRelayModeNative)
	default:
		err = fmt.Errorf("unsupport command type: %d", cmd.Type)
	}

	if err != nil {
		logrus.Errorf("onHandleDatagram err: %v", err)
	}
}

func (c *TUICClient) onHandleUniStream(stream quic.ReceiveStream) {
	var cmd protocol.Command
	err := cmd.Unmarshal(stream)
	if err != nil {
		logrus.Errorf("Failed to read command: %v", err)
		return
	}

	switch cmd.Type {
	case protocol.CmdPacket:
		err = c.packet(stream, cmd.Options.(*options.PacketOptions), protocol.UdpRelayModeQuic)
	default:
		err = fmt.Errorf("unsupport command type: %d", cmd.Type)
	}

	if err != nil {
		logrus.Errorf("onHandleDatagram err: %v", err)
	}
}

func (c *TUICClient) onHandleQUICPacket(data []byte, opts *options.PacketOptions) error {
	if opts.FragTotal > 1 {
		data = c.fragmentCache.AddFragment(opts.AssocID, opts.FragID, opts.FragTotal, opts.Size, data)
	}

	if data == nil {
		return nil
	}

	err := c.onHandlePacket(data, opts)
	if err != nil {
		return err
	}

	return nil
}

func (c *TUICClient) onHandlePacket(data []byte, opts *options.PacketOptions) error {
	conn := c.socketCacheMap[opts.AssocID]
	if conn == nil {
		return fmt.Errorf("conn not found, assocID: %d", opts.AssocID)
	}

	_, err := conn.Write(data)
	if err != nil {
		logrus.Errorf("Failed to write packet: %v", err)
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
