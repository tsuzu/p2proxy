# p2proxy
- A tool to create a reverse proxy via P2P

## License
- Under the MIT License
- Copyright (c) 2018 Tsuzu

## Usage
### Example

- connector
```
$ p2proxy -insecure -signaling XXXX.com:8000 -target 2022:22,2080:80 -key hoge -pass foobar
```

- listener
```
$ p2proxy -insecure -signaling XXXX.com:8000 -key hoge -pass foobar -listener
```

### Details
```
Usage of p2proxy:
  -client-cert string
        client certificate
  -client-key string
        client key
  -help
        show this
  -insecure
        allow an insecure connection
  -key string
        key
  -listener
        listener or connector
  -pass string
        password
  -public
        whether accepts connections from other computers(for connector)
  -server-cert string
        server certificate
  -signaling string
        signaling server
  -stun string
        STUN server(split with comma/don't include spaces) (default "stun.l.google.com:19302")
  -system-cert
        use system certificates
  -target string
        local_port:[remote_addr:]remote_port(split with comma to use multi ports, for connector)
  -token string
        if empty, try to register.(for listener)
```