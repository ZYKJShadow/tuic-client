package socket

import (
	"testing"
	"tuic-client/config"
)

func TestSockClient(t *testing.T) {
	client, err := NewSockClient(&config.SocksConfig{
		Server:   "127.0.0.1:7798",
		Ip:       "127.0.0.1",
		Username: "",
		Password: "",
	})

	if err != nil {
		t.Errorf("NewSockClient error: %v", err)
		return
	}

	err = client.Start()
	if err != nil {
		t.Errorf("client.Start error: %v", err)
		return
	}
}
