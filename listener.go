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
	stuns := strings.Split(*stun, ",")

	if len(stuns) == 1 && len(stuns[0]) == 0 {
		stuns = []string{}
	}

	conn := easyp2p.NewP2PConn(stuns)

	if _, err := conn.Listen(0); err != nil {
		return "", nil, errors.Wrap(err, "listen error")
	}

	if _, err := conn.DiscoverIP(); err != nil {
		return "", nil, errors.Wrap(err, "discovering IP addresses error")
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
	}

	for {
		res, err := listenClient.Recv()

		if err != nil {
			break
		}

		log.Println("listened:", res)

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

			if err := comm.Send(&api.CommunicateParameters{
				Param: &api.CommunicateParameters_Identifier{
					Identifier: res.Identifier,
				},
			}); err != nil {
				return
			}

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

			log.Println("initializing p2p connection")

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

			log.Printf("p2p connection established: %s<->%s", conn.RemoteAddr(), conn.LocalAddr())

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

				log.Printf("new stream accepted (remote addr: %s, id: %d)", conn.RemoteAddr(), stream.ID())
				wg.Add(1)
				go func() {
					defer wg.Done()
					defer stream.Close()

					var mut sync.Mutex
					var err error

					var idx int32
					if e := binary.Read(stream, binary.BigEndian, &idx); e != nil {
						err = errors.Wrap(err, "reading target id error")

						return
					}

					if idx < 0 || idx >= int32(len(targets)) {
						err = errors.New("invalid id: " + strconv.Itoa(int(idx)))

						return
					}

					dest := targets[idx].To + ":" + strconv.Itoa(targets[idx].ToPort)

					c, err := net.Dial("tcp", dest)

					if err != nil {
						err = errors.Wrap(err, "dialing target error")

						return
					}

					defer c.Close()

					la, ra := c.LocalAddr().String(), c.RemoteAddr().String()

					log.Printf("new proxy connection established (id: %d, remote addr: %s, %s<->%s)", stream.ID(), conn.RemoteAddr(), la, ra)

					var wg sync.WaitGroup

					fn := func(a, b net.Conn) {
						defer wg.Done()

						if _, e := io.Copy(a, b); e != nil {
							mut.Lock()
							err = e
							mut.Unlock()
						}

						a.Close()
						b.Close()
					}

					wg.Add(2)
					go fn(stream, c)
					go fn(c, stream)
					wg.Wait()

					log.Printf("proxy connection cllosed (id: %d, remote addr: %s, %s<->%s)", stream.ID(), conn.RemoteAddr(), la, ra)
				}()
			}
		}()
	}

	return nil
}
