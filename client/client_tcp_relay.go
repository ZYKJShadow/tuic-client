package client

import (
	"context"
	"errors"
	"github.com/ZYKJShadow/tuic-protocol-go/address"
	"github.com/ZYKJShadow/tuic-protocol-go/options"
	"github.com/ZYKJShadow/tuic-protocol-go/protocol"
	"github.com/quic-go/quic-go"
	"github.com/sirupsen/logrus"
	"io"
	"net"
	"time"
)

func (c *TUICClient) OnHandleTcpConnect(conn *net.TCPConn, remoteAddr address.Address) error {
	defer func() {
		_ = conn.Close()
	}()

	opts := &options.ConnectOptions{
		Addr: remoteAddr,
	}

	ctx, cancel := context.WithDeadline(c.ctx, time.Now().Add(time.Second*5))
	defer cancel()

	stream, err := c.conn.OpenStreamSync(ctx)
	if err != nil {
		logrus.Errorf("open stream failed: %v", err)
		return err
	}

	defer func() {
		_ = stream.Close()
		stream.CancelRead(protocol.NormalClosed)
		stream.CancelWrite(protocol.NormalClosed)
	}()

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

	go func() {
		_ = c.relay(stream, conn)
	}()

	// stream流无法读取的时候，表示服务端已经关闭连接，直接退出
	err = c.relay(conn, stream)
	if err != nil {
		logrus.Errorf("conn, stream error: %v", err)
	}

	return nil
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

func (c *TUICClient) relay(dst io.Writer, src io.Reader) error {
	buf := make([]byte, 32*1024)

	for {
		n, err := src.Read(buf)
		if err != nil {
			if err == io.EOF {
				return nil
			}

			var e *quic.StreamError
			if errors.As(err, &e) && e.ErrorCode == protocol.NormalClosed {
				return nil
			}

			return err
		}

		if n > 0 {
			n, err = dst.Write(buf[:n])
			if err != nil {
				return err
			}
		}
	}
}
