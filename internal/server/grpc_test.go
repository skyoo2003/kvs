package server

import (
	"context"
	"net"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	grpchealthv1 "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	"github.com/skyoo2003/kvs"
	"github.com/skyoo2003/kvs/api/kvsv1"
)

func TestGRPCServerPutGetDelete(t *testing.T) {
	const (
		key   = "language"
		value = "go"
	)

	conn := dialBufconn(t, kvs.NewStore())
	defer closeClientConn(t, conn)

	client := kvsv1.NewKVStoreClient(conn)
	ctx := context.Background()

	if _, err := client.Put(ctx, &kvsv1.PutRequest{Key: key, Value: value}); err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	getRes, err := client.Get(ctx, &kvsv1.GetRequest{Key: key})
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if getRes.GetKey() != key || getRes.GetValue() != value {
		t.Fatalf("Get() = %+v, want key/value", getRes)
	}

	if _, err := client.Delete(ctx, &kvsv1.DeleteRequest{Key: key}); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
}

func TestGRPCServerMissingKey(t *testing.T) {
	conn := dialBufconn(t, kvs.NewStore())
	defer closeClientConn(t, conn)

	client := kvsv1.NewKVStoreClient(conn)
	_, err := client.Get(context.Background(), &kvsv1.GetRequest{Key: "missing"})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("status.Code() = %v, want %v", status.Code(err), codes.NotFound)
	}
}

func TestGRPCServerDeleteMissingKey(t *testing.T) {
	conn := dialBufconn(t, kvs.NewStore())
	defer closeClientConn(t, conn)

	client := kvsv1.NewKVStoreClient(conn)
	_, err := client.Delete(context.Background(), &kvsv1.DeleteRequest{Key: "missing"})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("status.Code() = %v, want %v", status.Code(err), codes.NotFound)
	}
}

func TestGRPCServerPutOverwritesValue(t *testing.T) {
	const value = "rust"

	conn := dialBufconn(t, kvs.NewStore())
	defer closeClientConn(t, conn)

	client := kvsv1.NewKVStoreClient(conn)
	ctx := context.Background()

	if _, err := client.Put(ctx, &kvsv1.PutRequest{Key: "language", Value: "go"}); err != nil {
		t.Fatalf("Put(first) error = %v", err)
	}
	if _, err := client.Put(ctx, &kvsv1.PutRequest{Key: "language", Value: value}); err != nil {
		t.Fatalf("Put(second) error = %v", err)
	}

	res, err := client.Get(ctx, &kvsv1.GetRequest{Key: "language"})
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if res.GetValue() != value {
		t.Fatalf("value = %q, want %q", res.GetValue(), value)
	}
}

func TestGRPCServerSupportsSlashInKey(t *testing.T) {
	const key = "team/backend"

	conn := dialBufconn(t, kvs.NewStore())
	defer closeClientConn(t, conn)

	client := kvsv1.NewKVStoreClient(conn)
	ctx := context.Background()

	if _, err := client.Put(ctx, &kvsv1.PutRequest{Key: key, Value: "go"}); err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	res, err := client.Get(ctx, &kvsv1.GetRequest{Key: key})
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if res.GetKey() != key {
		t.Fatalf("key = %q, want %q", res.GetKey(), key)
	}
}

func TestGRPCServerHealth(t *testing.T) {
	conn := dialBufconn(t, kvs.NewStore())
	defer closeClientConn(t, conn)

	healthClient := grpchealthv1.NewHealthClient(conn)
	res, err := healthClient.Check(context.Background(), &grpchealthv1.HealthCheckRequest{})
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if res.GetStatus() != grpchealthv1.HealthCheckResponse_SERVING {
		t.Fatalf("status = %v, want %v", res.GetStatus(), grpchealthv1.HealthCheckResponse_SERVING)
	}
}

func dialBufconn(t *testing.T, store *kvs.Store) *grpc.ClientConn {
	t.Helper()

	listener := bufconn.Listen(1024 * 1024)
	server := NewGRPCServer(store)

	go func() {
		if err := server.Serve(listener); err != nil {
			_ = err
		}
	}()

	t.Cleanup(func() {
		server.Stop()
		_ = listener.Close()
	})

	conn, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return listener.Dial()
		}),
	)
	if err != nil {
		t.Fatalf("grpc.DialContext() error = %v", err)
	}

	return conn
}

func closeClientConn(t *testing.T, conn *grpc.ClientConn) {
	t.Helper()

	if err := conn.Close(); err != nil {
		t.Fatalf("conn.Close() error = %v", err)
	}
}
