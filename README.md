## tuic-client

使用go实现的tuic代理客户端，具体协议请查看[tuic-protocol-go](https://github.com/ZYKJShadow/tuic-protocol-go)，QUIC核心使用[quic-go](https://github.com/quic-go/quic-go)库实现，该库还有许多未知bug，仅供学习

客户端配置示例：
```json
{
  "client_config": {
    "server": "127.0.0.1:8888",
    "uuid": "0dcd8b80-603c-49dd-bfb7-61ebcfd5fbb8",
    "password": "0dcd8b80-603c-49dd-bfb7-61ebcfd5fbb8",
    "zero_rtt_handshake": true,
    "alpn": [
      "h3"
    ],
    "cert_path": "",
    "udp_relay_mode": ""
  },
  "socks_config": {
    "server": "127.0.0.1:7798",
    "ip": "127.0.0.1",
    "username": "",
    "password": "",
    "max_packet_size": 2048
  }
}
```
字段说明：
client_config:
1. server: 服务器地址
2. uuid: 服务器uuid
3. password: 服务器密码
4. zero_rtt_handshake: 是否启用0rtt
5. alpn:协议列表
6. cert_path: 证书路径

socks_config:
1. server: socks5服务器地址
2. ip: socks5服务器ip
3. username: socks5用户名
4. password: socks5密码
5. max_packet_size: 最大包大小