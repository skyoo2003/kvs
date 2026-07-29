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

func Run(ctx context.Context, cfg Config, store *kvs.Store) error {
	if store == nil {
		store = kvs.NewStore()
	}

	var lc net.ListenConfig

	httpListener, err := lc.Listen(ctx, "tcp", cfg.HTTPAddr)
	if err != nil {
		return fmt.Errorf("listen http: %w", err)
	}

	grpcListener, err := lc.Listen(ctx, "tcp", cfg.GRPCAddr)
	if err != nil {
		_ = httpListener.Close()
		return fmt.Errorf("listen grpc: %w", err)
	}

	return RunListeners(ctx, httpListener, grpcListener, store)
}

func RunListeners(ctx context.Context, httpListener, grpcListener net.Listener, store *kvs.Store) error {
	if store == nil {
		store = kvs.NewStore()
	}

	httpServer := &http.Server{
		Handler:           NewHTTPHandler(store),
		ReadHeaderTimeout: 5 * time.Second,
	}
	grpcServer := NewGRPCServer(store)
	errCh := make(chan error, 2)

	go func() {
		if err := httpServer.Serve(httpListener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("serve http: %w", err)
		}
	}()

	go func() {
		if err := grpcServer.Serve(grpcListener); err != nil {
			errCh <- fmt.Errorf("serve grpc: %w", err)
		}
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		grpcServer.GracefulStop()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown http: %w", err)
		}

		return nil
	case err := <-errCh:
		grpcServer.Stop()
		_ = httpServer.Close()
		return err
	}
}
