package config

import (
	"encoding/json"
	"errors"
)

type Config struct {
	SocksConfig  *SocksConfig  `json:"socks_config"`
	ClientConfig *ClientConfig `json:"client_config"`
}

type SocksConfig struct {
	Server        string `json:"server"`
	Ip            string `json:"ip"`
	Username      string `json:"username"`
	Password      string `json:"password"`
	MaxPacketSize int    `json:"max_packet_size"`
}

type ClientConfig struct {
	Server           string   `json:"server"`
	UUID             string   `json:"uuid"`
	Password         string   `json:"password"`
	CertPath         string   `json:"cert_path"`
	UDPRelayMode     string   `json:"udp_relay_mode"`
	ALPN             []string `json:"alpn"`
	ZeroRTTHandshake bool     `json:"zero_rtt_handshake"`
	MaxStreamCount   int64    `json:"max_stream_count"`
}

func (c *Config) Unmarshal(b []byte) error {
	return json.Unmarshal(b, c)
}

func (c *ClientConfig) SetDefaults() {
	c.ALPN = []string{"h3"}
	c.Server = "127.0.0.1:8888"
	c.UUID = "0dcd8b80-603c-49dd-bfb7-61ebcfd5fbb8"
	c.Password = "0dcd8b80-603c-49dd-bfb7-61ebcfd5fbb8"
	c.CertPath = ""
	c.ZeroRTTHandshake = false
}

func (c *ClientConfig) CheckValid() error {
	if len(c.UUID) < 16 {
		return errors.New("uuid too short, must be at least 16 bytes")
	}

	if c.Password == "" {
		return errors.New("password cannot be empty")
	}

	return nil
}

func (c *SocksConfig) SetDefaults() {
	c.Server = "127.0.0.1:7798"
	c.Ip = "127.0.0.1"
	c.Username = ""
	c.Password = ""
	c.MaxPacketSize = 2048
}
