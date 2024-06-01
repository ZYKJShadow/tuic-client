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
	"strings"
	"sync"
	"time"
)

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

func (c *TUICClient) onCloseStream(stream quic.Stream) {
	stream.CancelWrite(protocol.NormalClosed)
	stream.CancelRead(protocol.NormalClosed)
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
