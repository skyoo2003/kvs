package server

import (
	"bufio"
	"errors"
	"io"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/skyoo2003/kvs"
)

// respClient is a raw protocol client, so that the tests assert the bytes on the wire
// rather than whatever a client library chooses to send.
type respClient struct {
	t    *testing.T
	conn net.Conn
	br   *bufio.Reader
}

func newRESPClient(t *testing.T, store *kvs.Store) *respClient {
	t.Helper()

	return newRESPClientWithPassword(t, store, "")
}

func newRESPClientWithPassword(t *testing.T, store *kvs.Store, password string) *respClient {
	t.Helper()

	var lc net.ListenConfig
	listener, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}

	server := NewRESPServer(store, password)
	serveErr := make(chan error, 1)
	go func() {
		serveErr <- server.Serve(listener)
	}()

	var dialer net.Dialer
	conn, err := dialer.DialContext(t.Context(), "tcp", listener.Addr().String())
	if err != nil {
		_ = server.Close()
		_ = listener.Close()
		t.Fatalf("Dial() error = %v", err)
	}

	t.Cleanup(func() {
		_ = conn.Close()
		_ = server.Close()
		_ = listener.Close()

		if err := <-serveErr; err != nil {
			t.Errorf("Serve() error = %v", err)
		}
	})

	if err := conn.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
		t.Fatalf("SetDeadline() error = %v", err)
	}

	return newRESPClientOn(t, conn)
}

func newRESPClientOn(t *testing.T, conn net.Conn) *respClient {
	t.Helper()

	return &respClient{t: t, conn: conn, br: bufio.NewReader(conn)}
}

// itoa keeps the wire-format helpers in the tests readable.
func itoa(value int) string {
	return strconv.Itoa(value)
}

// readLineFor sends a command and returns its reply as a single line, for the cases where
// only the reply's prefix is being asserted.
func (c *respClient) readLineFor(args ...string) string {
	c.t.Helper()

	c.send(args...)

	return c.readLine()
}

// send writes args in the array form clients use.
func (c *respClient) send(args ...string) {
	c.t.Helper()

	var b strings.Builder
	b.WriteString("*" + strconv.Itoa(len(args)) + respCRLF)
	for _, arg := range args {
		b.WriteString("$" + strconv.Itoa(len(arg)) + respCRLF + arg + respCRLF)
	}

	c.writeRaw(b.String())
}

func (c *respClient) writeRaw(request string) {
	c.t.Helper()

	if _, err := c.conn.Write([]byte(request)); err != nil {
		c.t.Fatalf("Write(%q) error = %v", request, err)
	}
}

// expect reads exactly as many bytes as want and compares them.
func (c *respClient) expect(want string) {
	c.t.Helper()

	got := make([]byte, len(want))
	if _, err := io.ReadFull(c.br, got); err != nil {
		c.t.Fatalf("ReadFull() error = %v, want reply %q", err, want)
	}
	if string(got) != want {
		c.t.Fatalf("reply = %q, want %q", got, want)
	}
}

func (c *respClient) do(want string, args ...string) {
	c.t.Helper()

	c.send(args...)
	c.expect(want)
}

func (c *respClient) readLine() string {
	c.t.Helper()

	line, err := c.br.ReadString('\n')
	if err != nil {
		c.t.Fatalf("ReadString() error = %v", err)
	}

	return strings.TrimSuffix(line, respCRLF)
}

// readBulk reads a bulk string reply and returns its payload.
func (c *respClient) readBulk() string {
	c.t.Helper()

	header := c.readLine()
	if !strings.HasPrefix(header, "$") {
		c.t.Fatalf("reply header = %q, want a bulk string", header)
	}

	length, err := strconv.Atoi(header[1:])
	if err != nil {
		c.t.Fatalf("bulk length %q error = %v", header[1:], err)
	}
	if length < 0 {
		return ""
	}

	payload := make([]byte, length+len(respCRLF))
	if _, err := io.ReadFull(c.br, payload); err != nil {
		c.t.Fatalf("ReadFull(bulk) error = %v", err)
	}

	return string(payload[:length])
}

// readStringArray reads an array reply whose elements are all bulk strings.
func (c *respClient) readStringArray() []string {
	c.t.Helper()

	header := c.readLine()
	if !strings.HasPrefix(header, "*") {
		c.t.Fatalf("reply header = %q, want an array", header)
	}

	count, err := strconv.Atoi(header[1:])
	if err != nil {
		c.t.Fatalf("array length %q error = %v", header[1:], err)
	}
	if count <= 0 {
		return nil
	}

	items := make([]string, 0, count)
	for range count {
		items = append(items, c.readBulk())
	}

	return items
}

// readStringMap reads an array of alternating field and value elements as a map, so that a
// test does not depend on the order an unordered container reports its pairs in.
func (c *respClient) readStringMap() map[string]string {
	c.t.Helper()

	items := c.readStringArray()
	pairs := make(map[string]string, len(items)/2)
	for i := 0; i+1 < len(items); i += 2 {
		pairs[items[i]] = items[i+1]
	}

	return pairs
}

func (c *respClient) expectEOF() {
	c.t.Helper()

	if _, err := c.br.ReadByte(); !errors.Is(err, io.EOF) {
		c.t.Fatalf("ReadByte() error = %v, want %v", err, io.EOF)
	}
}

func TestRESPHandshakeCommands(t *testing.T) {
	client := newRESPClient(t, kvs.NewStore())

	client.do("+PONG"+respCRLF, "PING")
	client.do("$5"+respCRLF+"hello"+respCRLF, "PING", "hello")
	client.do("$2"+respCRLF+"hi"+respCRLF, "ECHO", "hi")
	client.do("*0"+respCRLF, "COMMAND", "DOCS")
	client.do("+OK"+respCRLF, "SELECT", "0")
	client.do("-ERR DB index is out of range"+respCRLF, "SELECT", "1")
	client.do("*2"+respCRLF+"$9"+respCRLF+"maxmemory"+respCRLF+"$1"+respCRLF+"0"+respCRLF,
		"CONFIG", "GET", "maxmemory")
	client.do("*0"+respCRLF, "CONFIG", "GET", "no-such-parameter")
}

func TestRESPHelloRefusesRESP3(t *testing.T) {
	client := newRESPClient(t, kvs.NewStore())

	client.do("-"+respErrNoProto+respCRLF, "HELLO", "3")

	// The handshake map is a flat array in RESP2: seven field and value pairs.
	client.send("HELLO", "2")
	client.expect("*14" + respCRLF)
	for _, want := range []string{"server", respServerName, "version", respRedisVersion, "proto", "2", "id"} {
		if got := client.readBulk(); got != want {
			t.Fatalf("HELLO field = %q, want %q", got, want)
		}
	}
	if id := client.readBulk(); id == "" {
		t.Fatal("HELLO id = empty, want the connection id")
	}
	for _, want := range []string{"mode", respModeStandalone, "role", respRoleMaster, "modules"} {
		if got := client.readBulk(); got != want {
			t.Fatalf("HELLO field = %q, want %q", got, want)
		}
	}
	client.expect("*0" + respCRLF)
}

func TestRESPClientAndInfo(t *testing.T) {
	client := newRESPClient(t, kvs.NewStore())

	client.do("$-1"+respCRLF, "CLIENT", "GETNAME")
	client.do("+OK"+respCRLF, "CLIENT", "SETNAME", "prober")
	client.do("$6"+respCRLF+"prober"+respCRLF, "CLIENT", "GETNAME")
	client.do("+OK"+respCRLF, "CLIENT", "SETINFO", "lib-name", "go-redis")
	client.do(":1"+respCRLF, "CLIENT", "ID")

	client.send("INFO")
	info := client.readBulk()
	for _, want := range []string{"redis_version:" + respRedisVersion, "server_name:" + respServerName, "role:master"} {
		if !strings.Contains(info, want) {
			t.Fatalf("INFO = %q, want it to contain %q", info, want)
		}
	}
	if !strings.Contains(info, "connected_clients:1") {
		t.Fatalf("INFO = %q, want connected_clients:1", info)
	}
}

func TestRESPRejectsBadRequests(t *testing.T) {
	client := newRESPClient(t, kvs.NewStore())

	client.do("-ERR unknown command 'NOPE'"+respCRLF, "NOPE")
	client.do("-ERR wrong number of arguments for 'echo' command"+respCRLF, "ECHO")
	client.do("-ERR wrong number of arguments for 'echo' command"+respCRLF, "ECHO", "a", "b")

	// A command name is client controlled and reaches the error reply, so a name carrying
	// CRLF must not be able to append a frame of its own.
	client.do("-ERR unknown command 'A  +INJECTED'"+respCRLF, "A\r\n+INJECTED")
}

func TestRESPClosesConnectionOnProtocolError(t *testing.T) {
	client := newRESPClient(t, kvs.NewStore())

	client.writeRaw("*1" + respCRLF + "+OK" + respCRLF)
	if line := client.readLine(); !strings.HasPrefix(line, "-ERR Protocol error:") {
		t.Fatalf("reply = %q, want a protocol error", line)
	}

	client.expectEOF()
}

func TestRESPQuitClosesConnection(t *testing.T) {
	client := newRESPClient(t, kvs.NewStore())

	client.do("+OK"+respCRLF, "QUIT")
	client.expectEOF()
}

func TestRESPInlineCommand(t *testing.T) {
	client := newRESPClient(t, kvs.NewStore())

	client.writeRaw("PING" + respCRLF)
	client.expect("+PONG" + respCRLF)
}

// TestRESPPipelinedBatchIsAnswered covers the flush path: replies are buffered, so a
// batch whose last command is split across writes must still be answered rather than
// leaving both sides waiting on each other.
func TestRESPPipelinedBatchIsAnswered(t *testing.T) {
	client := newRESPClient(t, kvs.NewStore())

	client.writeRaw(strings.Repeat("*1"+respCRLF+"$4"+respCRLF+"PING"+respCRLF, 3) + "*1" + respCRLF + "$4" + respCRLF)
	client.expect(strings.Repeat("+PONG"+respCRLF, 3))

	client.writeRaw("PING" + respCRLF)
	client.expect("+PONG" + respCRLF)
}
