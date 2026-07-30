package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/skyoo2003/kvs"
	"github.com/skyoo2003/kvs/api/kvsv1"
)

func TestRunListenersSharesStoreAcrossHTTPAndGRPC(t *testing.T) {
	const (
		key   = "language"
		value = "go"
	)

	httpListener, err := newTestListener(t)
	if err != nil {
		t.Fatalf("net.Listen(http) error = %v", err)
	}

	grpcListener, err := newTestListener(t)
	if err != nil {
		_ = httpListener.Close()
		t.Fatalf("net.Listen(grpc) error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- RunListeners(ctx, httpListener, grpcListener, kvs.NewStore())
	}()

	t.Cleanup(func() {
		cancel()
		select {
		case runErr := <-errCh:
			if runErr != nil {
				t.Fatalf("RunListeners() error = %v", runErr)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("RunListeners() did not stop")
		}
	})

	httpURL := "http://" + httpListener.Addr().String() + "/v1/keys/" + key
	putKeyOverHTTP(t, httpURL, value)

	grpcConn, err := grpc.NewClient(grpcListener.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc.DialContext() error = %v", err)
	}
	defer closeClientConn(t, grpcConn)

	client := kvsv1.NewKVStoreClient(grpcConn)
	getRes, err := client.Get(context.Background(), &kvsv1.GetRequest{Key: key})
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if getRes.GetValue() != value {
		t.Fatalf("Get().Value = %q, want %q", getRes.GetValue(), value)
	}

	assertHTTPStatus(t, http.MethodGet, "http://"+httpListener.Addr().String()+"/healthz", http.NoBody, http.StatusOK)

	body := mustReadKeyOverHTTP(t, httpURL)
	if body.Value != value {
		t.Fatalf("HTTP body = %+v, want value %q", body, value)
	}
}

func TestRunClosesHTTPListenerWhenGRPCListenFails(t *testing.T) {
	httpListener, err := newTestListener(t)
	if err != nil {
		t.Fatalf("net.Listen(http) error = %v", err)
	}
	httpAddr := httpListener.Addr().String()
	closeErr := httpListener.Close()
	if closeErr != nil {
		t.Fatalf("httpListener.Close() error = %v", closeErr)
	}

	grpcListener, err := newTestListener(t)
	if err != nil {
		t.Fatalf("net.Listen(grpc) error = %v", err)
	}
	defer func() {
		_ = grpcListener.Close()
	}()

	err = Run(context.Background(), Config{HTTPAddr: httpAddr, GRPCAddr: grpcListener.Addr().String()}, kvs.NewStore())
	if err == nil {
		t.Fatal("Run() error = nil, want listen grpc error")
	}
	if !strings.Contains(err.Error(), "listen grpc") {
		t.Fatalf("Run() error = %v, want grpc listen failure", err)
	}

	reopenedHTTPListener, err := newTestListenerAt(t, httpAddr)
	if err != nil {
		t.Fatalf("net.Listen(reopen http) error = %v", err)
	}
	_ = reopenedHTTPListener.Close()
}

func newTestListener(t *testing.T) (net.Listener, error) {
	t.Helper()

	return newTestListenerAt(t, "127.0.0.1:0")
}

func newTestListenerAt(t *testing.T, addr string) (net.Listener, error) {
	t.Helper()

	var lc net.ListenConfig

	return lc.Listen(t.Context(), "tcp", addr)
}

func putKeyOverHTTP(t *testing.T, url, value string) {
	t.Helper()

	mustDoRequest(t, http.MethodPut, url, bytes.NewBufferString(`{"value":"`+value+`"}`))
}

func assertHTTPStatus(t *testing.T, method, url string, body io.Reader, wantStatus int) {
	t.Helper()

	req, err := http.NewRequestWithContext(context.Background(), method, url, body)
	if err != nil {
		t.Fatalf("http.NewRequestWithContext() error = %v", err)
	}

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do(%s %s) error = %v", method, url, err)
	}
	defer func() {
		_ = res.Body.Close()
	}()

	if res.StatusCode != wantStatus {
		t.Fatalf("%s %s status = %d, want %d", method, url, res.StatusCode, wantStatus)
	}
}

func mustReadKeyOverHTTP(t *testing.T, url string) getResponse {
	t.Helper()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, http.NoBody)
	if err != nil {
		t.Fatalf("http.NewRequestWithContext() error = %v", err)
	}

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do(GET %s) error = %v", url, err)
	}
	defer func() {
		_ = res.Body.Close()
	}()

	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET status = %d, want %d", res.StatusCode, http.StatusOK)
	}

	var body getResponse
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}

	return body
}

func mustDoRequest(t *testing.T, method, url string, body *bytes.Buffer) {
	t.Helper()

	req, err := http.NewRequestWithContext(context.Background(), method, url, body)
	if err != nil {
		t.Fatalf("http.NewRequestWithContext() error = %v", err)
	}
	if method == http.MethodPut {
		req.Header.Set("Content-Type", "application/json")
	}

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do(%s %s) error = %v", method, url, err)
	}
	defer func() {
		_ = res.Body.Close()
	}()

	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("%s %s status = %d, want %d", method, url, res.StatusCode, http.StatusNoContent)
	}
}
