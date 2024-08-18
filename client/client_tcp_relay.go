package client

import (
	"errors"
	"github.com/ZYKJShadow/tuic-protocol-go/address"
	"github.com/ZYKJShadow/tuic-protocol-go/options"
	"github.com/ZYKJShadow/tuic-protocol-go/protocol"
	"github.com/quic-go/quic-go"
	"github.com/sirupsen/logrus"
	"io"
	"net"
	"sync"
	"time"
)

func (c *TUICClient) OnHandleTcpConnect(conn *net.TCPConn, remoteAddr address.Address) error {
	opts := &options.ConnectOptions{
		Addr: remoteAddr,
	}

	stream, err := c.conn.OpenStream()
	if err != nil {
		logrus.Errorf("open stream failed: %v", err)
		return err
	}

	defer func() {
		_ = conn.Close()
		_ = stream.Close()
	}()

	_ = stream.SetDeadline(time.Now().Add(time.Second * 5))
	_ = conn.SetDeadline(time.Now().Add(time.Second * 5))

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
		c.relay(conn, stream)
	}()

	go func() {
		defer wg.Done()
		c.relay(stream, conn)
	}()

	wg.Wait()

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

func (c *TUICClient) relay(dst io.Writer, src io.Reader) {
	var wg sync.WaitGroup
	buf := make(chan []byte, 32*1024)

	wg.Add(2)

	go func() {
		defer wg.Done()
		defer close(buf)

		var e *quic.StreamError

		for {
			b := make([]byte, 32*1024)
			n, err := src.Read(b)
			if err != nil && err != io.EOF {
				if errors.As(err, &e) && e.ErrorCode == protocol.NormalClosed {
					return
				}

				logrus.Errorf("Read err: %v", err)
				return
			}

			if err == io.EOF {
				return
			}

			if n <= 0 {
				return
			}

			buf <- b[:n]
		}
	}()

	go func() {
		defer wg.Done()
		for {
			select {
			case b, ok := <-buf:
				_, err := dst.Write(b)
				if err != nil {
					logrus.Errorf("Failed to write buf to stream: %v", err)
					return
				}

				if !ok {
					return
				}
			}
		}
	}()

	wg.Wait()
}
