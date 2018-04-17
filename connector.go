package main

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"log"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cs3238-tsuzu/go-easyp2p"
	"github.com/cs3238-tsuzu/sigserver/api"
	"github.com/pkg/errors"
	"github.com/xtaci/smux"
	"google.golang.org/grpc"
)

func connect(ctx context.Context, client api.ListenerClient, grpcConn *grpc.ClientConn) error {
	targets := make([]Forward, 0, 4)
	for _, v := range strings.Split(*target, ",") {
		s := strings.Split(v, ":")

		if len(s) != 3 {
			return errors.New("illegal target format")
		}
		from, err := strconv.ParseInt(s[0], 10, 32)
		if err != nil {
			return errors.Wrap(err, "illegal target format")
		}
		to, err := strconv.ParseInt(s[0], 10, 32)
		if err != nil {
			return errors.Wrap(err, "illegal target format")
		}

		targets = append(
			targets,
			Forward{
				From:   int(from),
				To:     s[1],
				ToPort: int(to),
			},
		)
	}

	conn := easyp2p.NewP2PConn(strings.Split(*stun, ","))

	defer conn.Close()

	if _, err := conn.Listen(0); err != nil {
		return errors.Wrap(err, "listen error")
	}

	if _, err := conn.DiscoverIP(); err != nil {
		return errors.Wrap(err, "Discovering IP addresses error")
	}

	localDescription, err := conn.LocalDescription()

	if err != nil {
		return errors.Wrap(err, "local description generation error")
	}

	connectResult, err := client.Connect(
		ctx,
		&api.ConnectParameters{
			Key: *key,
		},
	)

	if err != nil {
		return errors.Wrap(err, "connecting to the signlaling server error")
	}

	log.Println("connect result: ", connectResult.String())

	deadline, err := time.Parse(time.RFC3339, connectResult.Timelimit)

	if err != nil {
		return errors.Wrap(err, "time parsing error")
	}

	deadline = deadline.Add(5 * time.Second)

	commCtx, cancel := context.WithDeadline(ctx, deadline)

	defer cancel()

	comm, err := client.Communicate(commCtx)

	if err != nil {
		return err
	}

	defer comm.CloseSend()

	// Auth
	if err := comm.Send(&api.CommunicateParameters{
		Param: &api.CommunicateParameters_Identifier{
			Identifier: connectResult.Identifier,
		},
	}); err != nil {
		return errors.Wrap(err, "sending to the signaling server error")
	}

	if err := comm.Send(&api.CommunicateParameters{
		Param: &api.CommunicateParameters_Message{
			Message: localDescription,
		},
	}); err != nil {
		return errors.Wrap(err, "sending to the signaling server error")
	}

	if err := comm.Send(&api.CommunicateParameters{
		Param: &api.CommunicateParameters_Message{
			Message: *password,
		},
	}); err != nil {
		return errors.Wrap(err, "sending to the signaling server error")
	}

	recv, err := comm.Recv()
	if err != nil {
		return errors.Wrap(err, "receiving from the signaling server error")
	}

	if err := conn.Connect(ctx, recv.Message); err != nil {
		return err
	}

	grpcConn.Close()

	defaultConfig := smux.DefaultConfig()
	defaultConfig.KeepAliveInterval = 5 * time.Second
	defaultConfig.KeepAliveTimeout = 15 * time.Second

	session, err := smux.Client(conn, defaultConfig)

	if err != nil {
		return errors.Wrap(err, "creating stream muxer error")
	}

	if stream, err := session.OpenStream(); err != nil {
		return errors.Wrap(err, "opening stream error")
	} else {
		b, _ := json.Marshal(targets)

		if _, err := stream.Write(b); err != nil {
			stream.Close()
			return errors.Wrap(err, "target initialization error")
		}

		stream.Close()
	}

	var wg sync.WaitGroup

	fromBase := "127.0.0.1:"
	if *public {
		fromBase = "0.0.0.0:"
	}

	for i := range targets {
		wg.Add(1)
		go func(idx int, f *Forward) {
			defer wg.Done()

			var wg sync.WaitGroup

			defer wg.Wait()

			addr := fromBase + strconv.Itoa(f.From)
			listener, err := net.Listen("tcp", addr)

			if err != nil {
				log.Printf("%s: listen error", addr)

				return
			}

			wg.Add(1)
			go func() {
				defer wg.Done()

				<-ctx.Done()
				listener.Close()
			}()

			for {
				accepted, err := listener.Accept()

				if err != nil {
					log.Printf("%s: closed", addr)

					return
				}

				accepted.Close()

				wg.Add(1)
				go func() {
					defer accepted.Close()
					defer wg.Done()

					defer log.Printf("%s<->%s: closed", accepted.RemoteAddr().String())

					var wg sync.WaitGroup

					defer wg.Wait()

					stream, err := session.OpenStream()

					if err != nil {
						log.Printf("%s<->%s: %v", err)

						return
					}

					defer stream.Close()

					if err := binary.Write(stream, binary.BigEndian, int32(idx)); err != nil { // 4 bytes
						log.Printf("%s<->%s: %v", err)

						return
					}
					fn := func(a, b net.Conn) {
						defer wg.Done()

						io.Copy(a, b)

						a.Close()
						b.Close()
					}

					wg.Add(2)
					go fn(stream, conn)
					go fn(conn, stream)
				}()
			}
		}(i, &targets[i])
	}

	wg.Wait()

	return nil
}
