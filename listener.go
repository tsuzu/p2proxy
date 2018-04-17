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

	easyp2p "github.com/cs3238-tsuzu/go-easyp2p"
	"github.com/cs3238-tsuzu/sigserver/api"
	"github.com/pkg/errors"
	"github.com/xtaci/smux"
	"google.golang.org/grpc"
)

func initP2P() (string, *easyp2p.P2PConn, error) {
	conn := easyp2p.NewP2PConn(strings.Split(*stun, ","))

	defer conn.Close()

	if _, err := conn.Listen(0); err != nil {
		return "", nil, errors.Wrap(err, "listen error")
	}

	if _, err := conn.DiscoverIP(); err != nil {
		return "", nil, errors.Wrap(err, "Discovering IP addresses error")
	}

	localDescription, err := conn.LocalDescription()

	if err != nil {
		return "", nil, errors.Wrap(err, "local description generation error")
	}

	return localDescription, conn, nil
}

func listen(ctx context.Context, client api.ListenerClient, grpcConn *grpc.ClientConn) error {
	var wg sync.WaitGroup
	defer wg.Wait()

	token := *token
	if token == "" {
		res, err := client.Register(ctx, &api.RegisterParameters{
			Key: *key,
		})

		if err != nil {
			return errors.Wrap(err, "register error")
		}

		token = res.Token

		log.Printf("token: %s, max-age: %ds", token, res.MaxAge)
	}

	listenClient, err := client.Listen(ctx, &api.ListenParameters{
		Key:   *key,
		Token: token,
	})

	if err != nil {
		return errors.Wrap(err, "listen error")
	} else {
		for {
			res, err := listenClient.Recv()

			if err != nil {
				return errors.Wrap(err, "listen recv error")
			}

			wg.Add(1)
			go func() {
				defer wg.Done()

				commCtx := ctx
				var cancel func()
				if t, err := time.Parse(time.RFC3339, res.Timelimit); err == nil {
					commCtx, cancel = context.WithDeadline(commCtx, t)

					defer cancel()
				}

				comm, err := client.Communicate(ctx)

				if err != nil {
					return
				}

				defer comm.CloseSend()

				res, err := comm.Recv()

				if err != nil {
					return
				}

				remoteDescription := res.Message

				res, err = comm.Recv()

				if err != nil {
					return
				}

				if *password != res.Message {
					return
				}

				localDescription, conn, err := initP2P()

				if err != nil {
					return
				}

				if err := comm.Send(&api.CommunicateParameters{
					Param: &api.CommunicateParameters_Message{
						Message: localDescription,
					},
				}); err != nil {
					return
				}

				comm.CloseSend()

				if err := conn.Connect(ctx, remoteDescription); err != nil {
					return
				}
				defer conn.Close()

				defaultConfig := smux.DefaultConfig()
				defaultConfig.KeepAliveInterval = 5 * time.Second
				defaultConfig.KeepAliveTimeout = 15 * time.Second

				session, err := smux.Server(conn, defaultConfig)

				if err != nil {
					return
				}
				defer session.Close()

				var targets []Forward
				if stream, err := session.AcceptStream(); err != nil {
					return
				} else {
					if err := json.NewDecoder(stream).Decode(&targets); err != nil {
						stream.Close()

						return
					}
					stream.Close()
				}

				var wg sync.WaitGroup
				defer wg.Wait()

				for {
					stream, err := session.AcceptStream()

					if err != nil {
						return
					}

					wg.Add(1)
					go func() {
						defer wg.Done()
						defer stream.Close()

						var idx int32
						if binary.Read(stream, binary.BigEndian, &idx) != nil {
							return
						}

						if idx < 0 || idx >= int32(len(targets)) {
							return
						}

						c, err := net.Dial("tcp", targets[idx].To+":"+strconv.Itoa(targets[idx].ToPort))

						if err != nil {
							return
						}

						fn := func(a, b net.Conn) {
							defer wg.Done()

							io.Copy(a, b)

							a.Close()
							b.Close()
						}

						wg.Add(2)
						go fn(stream, c)
						go fn(c, stream)
					}()
				}
			}()
		}
	}

	return nil
}
