package main

import (
	"context"
	"flag"
	"github.com/sirupsen/logrus"
	"os"
	"tuic-client/client"
	"tuic-client/config"
	"tuic-client/socket"
)

var path string

func init() {
	flag.StringVar(&path, "c", "", "config file path")
	flag.StringVar(&path, "config", "", "config file path")
}

func main() {

	flag.Parse()
	logrus.SetReportCaller(true)

	clientCfg := &config.ClientConfig{}
	socksConfig := &config.SocksConfig{}
	c := &client.TUICClient{}

	if path != "" {
		b, err := os.ReadFile(path)
		if err != nil {
			panic(err)
		}

		var cfg config.Config
		err = cfg.Unmarshal(b)
		if err != nil {
			panic(err)
		}

		clientCfg = cfg.ClientConfig
		socksConfig = cfg.SocksConfig
	} else {
		clientCfg.SetDefaults()
		socksConfig.SetDefaults()
	}

	c, err := client.NewTUICClient(context.Background(), clientCfg)
	if err != nil {
		panic(err)
	}

	err = c.Start()
	if err != nil {
		panic(err)
	}

	server, err := socket.NewSockServer(socksConfig)
	if err != nil {
		panic(err)
	}

	err = server.Start()
	if err != nil {
		panic(err)
	}
}
