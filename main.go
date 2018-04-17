package main

import (
	"context"
	"flag"
	"log"

	"github.com/cs3238-tsuzu/sigserver/api"

	"google.golang.org/grpc"
)

var (
	insecure  = flag.Bool("insecure", false, "allow a insecure connection")
	listener  = flag.Bool("listener", false, "listener or connector")
	signaling = flag.String("signaling", "", "signaling server")
	key       = flag.String("key,k", "", "key")
	password  = flag.String("pass,p", "", "password")
	token     = flag.String("token", "", "if empty, try to register.(only needed for listener mode)")
	stun      = flag.String("stun", "stun.l.google.com:19302", "STUN server(split with comma/don't include spaces)")
	target    = flag.String("target,t", "", "for connector. local_port:remote_addr:remote_port(split with comma to use multi ports)")
	public    = flag.Bool("public,p", false, "for connector. whether accepts connections from other computers")
)

func main() {
	flag.Parse()

	ctx := context.Background()

	dialOpts := []grpc.DialOption{}

	if *insecure {
		dialOpts = append(dialOpts, grpc.WithInsecure())
	}

	conn, err := grpc.DialContext(ctx, *signaling, dialOpts...)

	if err != nil {
		log.Println("dial error:", err)

		return
	}

	defer conn.Close()

	client := api.NewListenerClient(conn)

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
