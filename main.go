package main

import (
	"context"
	"crypto/x509"
	"flag"
	"log"
	"time"

	"google.golang.org/grpc/keepalive"

	"google.golang.org/grpc/credentials"

	"github.com/cs3238-tsuzu/sigserver/api"

	"google.golang.org/grpc"
)

var (
	insecure   = flag.Bool("insecure", false, "allow an insecure connection")
	systemCert = flag.Bool("system-cert", false, "use system certificates")
	serverCert = flag.String("server-cert", "", "server certificate")
	clientKey  = flag.String("client-key", "", "client key")
	clientCert = flag.String("client-cert", "", "client certificate")
	listener   = flag.Bool("listener", false, "listener or connector")
	signaling  = flag.String("signaling", "", "signaling server")
	key        = flag.String("key", "", "key")
	password   = flag.String("pass", "", "password")
	token      = flag.String("token", "", "if empty, try to register.(for listener)")
	stun       = flag.String("stun", "stun.l.google.com:19302", "STUN server(split with comma/don't include spaces)")
	target     = flag.String("target", "", "local_port:[remote_addr:]remote_port(split with comma to use multi ports, for connector)")
	public     = flag.Bool("public", false, "whether accepts connections from other computers(for connector)")
	help       = flag.Bool("help", false, "show this")
)

func main() {
	flag.Parse()

	if *help {
		flag.Usage()

		return
	}

	ctx := context.Background()

	dialOpts := []grpc.DialOption{}

	if *systemCert {
		pool, err := x509.SystemCertPool()

		if err != nil {
			log.Fatal("system cert pool error:", err)
		}
		cred := credentials.NewClientTLSFromCert(pool, "")

		dialOpts = append(dialOpts, grpc.WithTransportCredentials(cred))
	}

	if serverCert := *serverCert; serverCert != "" {
		cred, err := credentials.NewClientTLSFromFile(serverCert, "")

		if err != nil {
			log.Fatal("server certificate loading error:", err)
		}

		dialOpts = append(dialOpts, grpc.WithTransportCredentials(cred))
	}

	if clientCert := *clientCert; clientCert != "" {
		// TODO:		credentials.NewServerTLSFromFile(certFile string, keyFile string)
	}

	if *insecure {
		dialOpts = append(dialOpts, grpc.WithInsecure())
	}

	dialOpts = append(dialOpts, grpc.WithKeepaliveParams(keepalive.ClientParameters{
		Time:                1 * time.Minute,
		PermitWithoutStream: true,
	}))

	conn, err := grpc.DialContext(ctx, *signaling, dialOpts...)

	if err != nil {
		log.Println("dial error:", err)

		return
	}

	defer conn.Close()

	client := api.NewListenerClient(conn)

	go func() {
		ticker := time.NewTicker(15 * time.Second)

		for {
			select {
			case <-ticker.C:
			case <-ctx.Done():
				return
			}

			_, err := client.Ping(ctx, &api.PingData{Text: "ping"})

			if err != nil {
				log.Println("ping error!:", err)

				return
			}
		}
	}()

	if *listener {
		if err := listen(ctx, client, conn); err != nil {
			log.Fatal("listen error: ", err)
		}
	} else {
		if err := connect(ctx, client, conn); err != nil {
			log.Fatal("connect error: ", err)
		}
	}
}
