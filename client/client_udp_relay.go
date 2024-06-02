package client

import (
	"context"
	"fmt"
	"github.com/ZYKJShadow/tuic-protocol-go/address"
	"github.com/ZYKJShadow/tuic-protocol-go/options"
	"github.com/ZYKJShadow/tuic-protocol-go/protocol"
	"github.com/sirupsen/logrus"
	"net"
	"time"
)

func (c *TUICClient) OnHandleUdpConnect(udp *net.UDPConn, assocID uint16, data []byte, remoteAddr address.Address) {
	c.Lock()
	_, ok := c.socketCacheMap[assocID]
	if !ok {
		c.socketCacheMap[assocID] = udp
	}
	c.Unlock()

	opts := &options.PacketOptions{
		AssocID:   assocID,
		FragTotal: 1,
		FragID:    0,
		Size:      0,
		Addr:      remoteAddr,
	}

	opts.CalFragTotal(data, c.MaxPacketSize)
	switch {
	case opts.FragTotal > 1:
		c.onRelayFragmentedUdpSend(data, opts)
	default:
		c.onRelayUdpSend(data, opts)
	}
}

func (c *TUICClient) onRelayFragmentedUdpSend(data []byte, opts *options.PacketOptions) {
	// 确保即使 len(data) 不能被 opts.FragTotal 整除也能正确处理
	fragSize := (len(data) + int(opts.FragTotal) - 1) / int(opts.FragTotal)
	opts.Size = uint16(fragSize)

	for i := 0; i < int(opts.FragTotal); i++ {
		opts.FragID = uint8(i)
		start := i * fragSize
		end := start + fragSize
		if end > len(data) {
			end = len(data)
		}
		c.onRelayUdpSend(data[start:end], opts)
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
		err = c.onSendUniStream(cmdBytes)
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

func (c *TUICClient) onSendUniStream(data []byte) error {
	ctx, cancel := context.WithDeadline(c.ctx, time.Now().Add(time.Second*3))
	defer cancel()

	stream, err := c.conn.OpenUniStreamSync(ctx)
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
