package server

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"time"

	"github.com/skyoo2003/kvs"
	"github.com/skyoo2003/kvs/internal/cluster"
)

// Listeners carries the bound listeners RunListeners serves. A nil field disables that
// protocol.
type Listeners struct {
	HTTP net.Listener
	GRPC net.Listener
	RESP net.Listener
}

// OpenStore builds the store the three protocols share. A DataDir turns on the append log, so
// the keyspace survives a restart; without one the store is in memory, which is what kvs did
// before and still does unless asked otherwise.
//
// The caller closes it.
//
//nolint:gocritic // Settings read once at startup; a pointer would only invite mutation.
func OpenStore(cfg Config) (*kvs.Store, error) {
	store := kvs.NewStore()
	store.SetCodec(respCodec{})

	// In a cluster the Raft log is the durable record, and a second log of the same changes
	// would only be a second thing to keep in step.
	if cfg.DataDir != "" && cfg.RaftAddr == "" {
		opened, err := kvs.Open(cfg.DataDir, respCodec{})
		if err != nil {
			return nil, fmt.Errorf("open data dir %s: %w", cfg.DataDir, err)
		}
		store = opened
	}

	return store, nil
}

//nolint:gocritic // Settings read once at startup; a pointer would only invite mutation.
func Run(ctx context.Context, cfg Config, store *kvs.Store) error {
	// The cluster comes up before the listeners, so a node is already voting by the time it
	// answers anyone.
	node, clusterErr := startCluster(ctx, cfg, store)
	if clusterErr != nil {
		return clusterErr
	}
	if node != nil {
		defer func() { _ = node.Close() }()
	}

	var lc net.ListenConfig
	var listeners Listeners

	opened := make([]net.Listener, 0, 3)
	listen := func(name, addr string) (net.Listener, error) {
		listener, err := lc.Listen(ctx, "tcp", addr)
		if err != nil {
			for _, open := range opened {
				_ = open.Close()
			}

			return nil, fmt.Errorf("listen %s: %w", name, err)
		}
		opened = append(opened, listener)

		return listener, nil
	}

	var err error
	// RESP binds first so that a tolerated failure has no other listener to unwind.
	if cfg.RESPAddr != "" {
		if listeners.RESP, err = listen("resp", cfg.RESPAddr); err != nil {
			// The RESP listener is on by default, so a machine already running Redis on 6379
			// would otherwise lose the HTTP and gRPC servers too, on an address nobody chose.
			// An address the operator did name is theirs to get wrong, and still fatal.
			if cfg.RESPAddr != DefaultConfig().RESPAddr {
				return fmt.Errorf(`%w (pick another --resp-addr, or "none" to disable it)`, err)
			}

			log.Printf(`kvs: %v; RESP is off (set --resp-addr to move it, or "none" to silence this)`, err)
		}
	}
	if listeners.HTTP, err = listen("http", cfg.HTTPAddr); err != nil {
		return err
	}
	if listeners.GRPC, err = listen("grpc", cfg.GRPCAddr); err != nil {
		return err
	}

	return runListeners(ctx, store, listeners, cfg.RESPPassword, membership(node))
}

func RunListeners(ctx context.Context, store *kvs.Store, listeners Listeners) error {
	return runListeners(ctx, store, listeners, "", nil)
}

// startCluster stands this node up as a cluster member, or reports that there is no cluster to
// join. A RaftAddr is what asks for one.
//
//nolint:gocritic // Settings read once at startup; a pointer would only invite mutation.
func startCluster(ctx context.Context, cfg Config, store *kvs.Store) (*cluster.Node, error) {
	if cfg.RaftAddr == "" {
		return nil, nil
	}
	if cfg.DataDir == "" {
		return nil, errors.New("a cluster needs --data-dir: the raft log has to live somewhere")
	}
	// Raft identifies nodes by this, and clients are sent to it by name, so it cannot be blank.
	if cfg.NodeID == "" {
		return nil, errors.New("a cluster needs --node-id when there is no --resp-addr to borrow")
	}

	node, err := cluster.Start(cluster.Config{
		NodeID:   cfg.NodeID,
		RaftAddr: cfg.RaftAddr,
		DataDir:  cfg.DataDir,
		// Exactly one node starts the cluster, and it is the one with nobody to join.
		Bootstrap: cfg.JoinAddr == "",
	}, store)
	if err != nil {
		return nil, err
	}

	if cfg.JoinAddr != "" {
		go joinCluster(ctx, cfg.JoinAddr, cfg.RESPPassword, cfg.NodeID, cfg.RaftAddr)
	}

	return node, nil
}

// membership converts a possibly absent node into an interface that is genuinely nil when there
// is none. Assigning a nil *cluster.Node straight into the interface would not be.
func membership(node *cluster.Node) clusterNode {
	if node == nil {
		return nil
	}

	return node
}

func runListeners(
	ctx context.Context, store *kvs.Store, listeners Listeners, respPassword string, node clusterNode,
) error {
	if store == nil {
		store = kvs.NewStore()
	}

	httpServer := &http.Server{
		Handler:           NewHTTPHandler(store),
		ReadHeaderTimeout: 5 * time.Second,
	}
	grpcServer := NewGRPCServer(store)
	respServer := NewRESPServer(store, respPassword)
	if node != nil {
		respServer.SetCluster(node)
	}
	errCh := make(chan error, 3)

	if listeners.HTTP != nil {
		go func() {
			if err := httpServer.Serve(listeners.HTTP); err != nil && !errors.Is(err, http.ErrServerClosed) {
				errCh <- fmt.Errorf("serve http: %w", err)
			}
		}()
	}

	if listeners.GRPC != nil {
		go func() {
			if err := grpcServer.Serve(listeners.GRPC); err != nil {
				errCh <- fmt.Errorf("serve grpc: %w", err)
			}
		}()
	}

	if listeners.RESP != nil {
		go func() {
			if err := respServer.Serve(listeners.RESP); err != nil {
				errCh <- fmt.Errorf("serve resp: %w", err)
			}
		}()
	}

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		grpcServer.GracefulStop()
		_ = respServer.Close()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown http: %w", err)
		}

		return nil
	case err := <-errCh:
		grpcServer.Stop()
		_ = respServer.Close()
		_ = httpServer.Close()

		return err
	}
}
