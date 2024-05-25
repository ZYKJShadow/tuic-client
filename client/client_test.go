package client

import (
	"context"
	"testing"
	"tuic-client/config"
)

func TestClient(t *testing.T) {
	c := &config.ClientConfig{}
	c.SetDefaults()

	_, err := NewTUICClient(context.Background(), c)
	if err != nil {
		t.Errorf("NewTUICClient error: %v", err)
		return
	}

	select {}
}
