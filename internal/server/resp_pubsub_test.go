package server

import (
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/skyoo2003/kvs"
)

// newRESPClients opens count clients against one server, which is what pub/sub needs.
func newRESPClients(t *testing.T, count int) []*respClient {
	t.Helper()

	var lc net.ListenConfig
	listener, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}

	server := NewRESPServer(kvs.NewStore(), "")
	serveErr := make(chan error, 1)
	go func() {
		serveErr <- server.Serve(listener)
	}()

	t.Cleanup(func() {
		_ = server.Close()
		_ = listener.Close()

		if err := <-serveErr; err != nil {
			t.Errorf("Serve() error = %v", err)
		}
	})

	clients := make([]*respClient, 0, count)
	for range count {
		var dialer net.Dialer
		conn, dialErr := dialer.DialContext(t.Context(), "tcp", listener.Addr().String())
		if dialErr != nil {
			t.Fatalf("Dial() error = %v", dialErr)
		}
		if deadlineErr := conn.SetDeadline(time.Now().Add(20 * time.Second)); deadlineErr != nil {
			t.Fatalf("SetDeadline() error = %v", deadlineErr)
		}
		t.Cleanup(func() { _ = conn.Close() })

		clients = append(clients, newRESPClientOn(t, conn))
	}

	return clients
}

// respMessageFrame is the wire form of a message pushed to a channel subscriber.
func respMessageFrame(channel, payload string) string {
	return "*3" + respCRLF + "$7" + respCRLF + "message" + respCRLF +
		"$" + itoa(len(channel)) + respCRLF + channel + respCRLF +
		"$" + itoa(len(payload)) + respCRLF + payload + respCRLF
}

func respSubscribeFrame(kind, name string, count int) string {
	return "*3" + respCRLF +
		"$" + itoa(len(kind)) + respCRLF + kind + respCRLF +
		"$" + itoa(len(name)) + respCRLF + name + respCRLF +
		":" + itoa(count) + respCRLF
}

func TestRESPSubscribeAndPublish(t *testing.T) {
	clients := newRESPClients(t, 2)
	subscriber, publisher := clients[0], clients[1]

	subscriber.do(respSubscribeFrame("subscribe", "news", 1), "SUBSCRIBE", "news")

	publisher.do(":1"+respCRLF, "PUBLISH", "news", "hello")
	subscriber.expect(respMessageFrame("news", "hello"))

	// Nobody listens on another channel.
	publisher.do(":0"+respCRLF, "PUBLISH", "other", "ignored")

	subscriber.do(respSubscribeFrame("unsubscribe", "news", 0), "UNSUBSCRIBE", "news")
	publisher.do(":0"+respCRLF, "PUBLISH", "news", "gone")
}

func TestRESPPatternSubscribe(t *testing.T) {
	clients := newRESPClients(t, 2)
	subscriber, publisher := clients[0], clients[1]

	subscriber.do(respSubscribeFrame("psubscribe", "news.*", 1), "PSUBSCRIBE", "news.*")

	publisher.do(":1"+respCRLF, "PUBLISH", "news.sport", "goal")
	subscriber.expect("*4" + respCRLF + "$8" + respCRLF + "pmessage" + respCRLF +
		"$6" + respCRLF + "news.*" + respCRLF + "$10" + respCRLF + "news.sport" + respCRLF +
		"$4" + respCRLF + "goal" + respCRLF)

	publisher.do(":0"+respCRLF, "PUBLISH", "weather", "rain")

	subscriber.do(respSubscribeFrame("punsubscribe", "news.*", 0), "PUNSUBSCRIBE")
}

// TestRESPSubscribeModeRestrictsCommands covers the RESP2 rule that a subscribed connection
// accepts only the subscribe family plus PING, QUIT, and RESET. PUBLISH is refused as well,
// which is why one connection can never be both a publisher and a subscriber.
func TestRESPSubscribeModeRestrictsCommands(t *testing.T) {
	client := newRESPClient(t, kvs.NewStore())

	client.do(respSubscribeFrame("subscribe", "news", 1), "SUBSCRIBE", "news")

	for _, args := range [][]string{{"GET", "k"}, {"PUBLISH", "news", "x"}, {"MULTI"}} {
		line := client.readLineFor(args...)
		want := "-ERR Can't execute '" + strings.ToLower(args[0]) + "'"
		if !strings.HasPrefix(line, want) {
			t.Fatalf("%v while subscribed = %q, want prefix %q", args, line, want)
		}
	}

	// PING answers as an array while subscribed, matching the pushed message shape.
	client.do("*2"+respCRLF+"$4"+respCRLF+"pong"+respCRLF+"$0"+respCRLF+respCRLF, "PING")

	// RESET leaves subscribe mode, after which ordinary commands work again.
	client.do("+RESET"+respCRLF, "RESET")
	client.do("$-1"+respCRLF, "GET", "k")
}

func TestRESPUnsubscribeWithNoSubscriptions(t *testing.T) {
	client := newRESPClient(t, kvs.NewStore())

	client.do("*3"+respCRLF+"$11"+respCRLF+"unsubscribe"+respCRLF+"$-1"+respCRLF+":0"+respCRLF, "UNSUBSCRIBE")
}

// TestRESPConcurrentPublishDeliversEveryMessage drives the broker from several connections at
// once. Delivery is queued rather than written inline, so a publisher never blocks on a
// subscriber's writer; if that changed, this test would hang instead of reporting a bad
// reply. It also checks that nothing is dropped while both sides run flat out.
func TestRESPConcurrentPublishDeliversEveryMessage(t *testing.T) {
	const (
		rounds      = 100
		subscribers = 2
		publishers  = 2
	)

	clients := newRESPClients(t, subscribers+publishers)
	subs, pubs := clients[:subscribers], clients[subscribers:]

	for _, sub := range subs {
		sub.do(respSubscribeFrame("subscribe", "news", 1), "SUBSCRIBE", "news")
	}

	var wg sync.WaitGroup

	for i, pub := range pubs {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for range rounds {
				pub.send("PUBLISH", "news", "ping")
				if line := pub.readLine(); line != ":"+itoa(subscribers) {
					t.Errorf("publisher %d: PUBLISH = %q, want %d receivers", i, line, subscribers)

					return
				}
			}
		}()
	}

	for i, sub := range subs {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for range rounds * publishers {
				sub.expect(respMessageFrame("news", "ping"))
			}
			_ = i
		}()
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("concurrent publishing did not finish, want delivery never to block a publisher")
	}
}

// TestRESPClosedSubscriberStopsCountingAsReceiver checks that a dropped connection is removed
// from the broker rather than lingering as a phantom subscriber.
func TestRESPClosedSubscriberStopsCountingAsReceiver(t *testing.T) {
	clients := newRESPClients(t, 2)
	subscriber, publisher := clients[0], clients[1]

	subscriber.do(respSubscribeFrame("subscribe", "news", 1), "SUBSCRIBE", "news")
	publisher.do(":1"+respCRLF, "PUBLISH", "news", "first")

	if err := subscriber.conn.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	// The server only learns about the close on its own read, so retry until it settles.
	deadline := time.Now().Add(10 * time.Second)
	for {
		publisher.send("PUBLISH", "news", "second")
		if line := publisher.readLine(); line == ":0" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("PUBLISH still reports a receiver after the subscriber closed")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
