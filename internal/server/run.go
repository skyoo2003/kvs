package server

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/skyoo2003/kvs"
)

// Listeners carries the bound listeners RunListeners serves. A nil field disables that
// protocol.
type Listeners struct {
	HTTP net.Listener
	GRPC net.Listener
	RESP net.Listener
}

func Run(ctx context.Context, cfg Config, store *kvs.Store) error {
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
	if listeners.HTTP, err = listen("http", cfg.HTTPAddr); err != nil {
		return err
	}
	if listeners.GRPC, err = listen("grpc", cfg.GRPCAddr); err != nil {
		return err
	}
	if cfg.RESPAddr != "" {
		if listeners.RESP, err = listen("resp", cfg.RESPAddr); err != nil {
			return err
		}
	}

	return runListeners(ctx, store, listeners, cfg.RESPPassword)
}

func RunListeners(ctx context.Context, store *kvs.Store, listeners Listeners) error {
	return runListeners(ctx, store, listeners, "")
}

func runListeners(ctx context.Context, store *kvs.Store, listeners Listeners, respPassword string) error {
	if store == nil {
		store = kvs.NewStore()
	}

	httpServer := &http.Server{
		Handler:           NewHTTPHandler(store),
		ReadHeaderTimeout: 5 * time.Second,
	}
	grpcServer := NewGRPCServer(store)
	respServer := NewRESPServer(store, respPassword)
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
