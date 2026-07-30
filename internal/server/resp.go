package server

import (
	"crypto/subtle"
	"errors"
	"fmt"
	"maps"
	"net"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/skyoo2003/kvs"
	"github.com/skyoo2003/kvs/pkg/resp"
)

const (
	respOK   = "OK"
	respPong = "PONG"

	// respRedisVersion is what clients see in INFO and HELLO. Tooling gates features on it,
	// so it names the Redis dialect kvs speaks rather than the kvs release.
	respRedisVersion = "7.4.0"
	respServerName   = "kvs"

	respErrSyntax     = "ERR syntax error"
	respErrNotInteger = "ERR value is not an integer or out of range"
	respErrWrongType  = "WRONGTYPE Operation against a key holding the wrong kind of value"
	respErrDecrOver   = "ERR decrement would overflow"
	respErrNoAuth     = "NOAUTH Authentication required."
	respErrWrongPass  = "WRONGPASS invalid username-password pair or user is disabled."

	// respPushDepth and respPushBytes bound one subscriber's backlog by both message count
	// and memory, the way Redis bounds a client output buffer. A subscriber that exceeds
	// either is disconnected, which is what keeps a publisher's cost independent of how
	// fast its subscribers read.
	respPushDepth = 1024
	respPushBytes = 8 * 1024 * 1024
)

// errRESPQuit reports that the client asked to end the session, so the connection should
// close once its reply is flushed.
var errRESPQuit = errors.New("resp: client quit")

// respHandler runs one command. args holds the command name followed by its arguments.
type respHandler func(c *respConn, args [][]byte) error

// respCommand describes one entry of the dispatch table. maxArgs is -1 when the command
// takes a variable number of arguments. Both bounds count the command name itself.
type respCommand struct {
	run     respHandler
	minArgs int
	maxArgs int
}

// RESPServer serves Redis and Valkey clients speaking RESP2 against a kvs.Store.
type RESPServer struct {
	store    *kvs.Store
	password string
	broker   *respBroker
	lastID   atomic.Int64

	mu     sync.Mutex
	conns  map[*respConn]struct{}
	closed bool
}

// NewRESPServer creates a server backed by store, or by a fresh store when store is nil. An
// empty password leaves the server open, matching how Redis behaves without requirepass.
func NewRESPServer(store *kvs.Store, password string) *RESPServer {
	if store == nil {
		store = kvs.NewStore()
	}

	return &RESPServer{
		store:    store,
		password: password,
		broker:   newRESPBroker(),
		conns:    make(map[*respConn]struct{}),
	}
}

// Serve accepts clients until listener is closed or Close is called. A graceful close
// reports no error.
func (s *RESPServer) Serve(listener net.Listener) error {
	for {
		netConn, err := listener.Accept()
		if err != nil {
			if s.isClosed() {
				return nil
			}

			return fmt.Errorf("accept resp: %w", err)
		}

		conn := s.newConn(netConn)
		if !s.track(conn) {
			_ = netConn.Close()

			return nil
		}

		go func() {
			defer s.untrack(conn)

			conn.serve()
		}()
	}
}

// Close stops accepting clients and drops the connections that are still open, which
// unblocks their goroutines. It is safe to call more than once.
func (s *RESPServer) Close() error {
	s.mu.Lock()
	s.closed = true
	conns := slices.Collect(maps.Keys(s.conns))
	clear(s.conns)
	s.mu.Unlock()

	for _, conn := range conns {
		_ = conn.netConn.Close()
	}

	return nil
}

func (s *RESPServer) newConn(netConn net.Conn) *respConn {
	conn := &respConn{
		server:  s,
		netConn: netConn,
		writer:  resp.NewWriter(netConn),
		id:      s.lastID.Add(1),
		authed:  s.password == "",
		pushes:  make(chan respPush, respPushDepth),
		done:    make(chan struct{}),
	}
	// Replies are buffered, so they must reach the client before a read can block. Wiring
	// the flush into the read path covers that for every case, including a pipelined batch
	// whose last command arrives split across TCP segments.
	conn.reader = resp.NewReader(flushBeforeRead{conn: conn})

	return conn
}

func (s *RESPServer) track(conn *respConn) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return false
	}

	s.conns[conn] = struct{}{}

	return true
}

func (s *RESPServer) untrack(conn *respConn) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.conns, conn)
}

func (s *RESPServer) isClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.closed
}

func (s *RESPServer) connCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return len(s.conns)
}

// flushBeforeRead sends pending replies whenever the protocol reader is about to wait on
// the socket. A buffered reader only reaches its underlying reader when it cannot satisfy a
// read from its buffer, which is exactly when the client is owed a reply.
type flushBeforeRead struct {
	conn *respConn
}

func (f flushBeforeRead) Read(p []byte) (int, error) {
	if err := f.conn.flush(); err != nil {
		return 0, err
	}

	return f.conn.netConn.Read(p)
}

// respConn is one client session.
type respConn struct {
	server  *RESPServer
	netConn net.Conn
	reader  *resp.Reader
	writer  *resp.Writer
	id      int64
	name    string
	authed  bool

	// writeMu guards the writer. Pushed pub/sub messages are written by a second goroutine,
	// so a reply must be able to claim the writer for as long as it takes to write it whole.
	writeMu sync.Mutex

	// pushes carries pub/sub messages queued by other connections, and done closes when the
	// session ends. Queueing rather than writing inline is what keeps two publishing
	// connections from ever waiting on each other's writeMu.
	pushes    chan respPush
	pushBytes atomic.Int64
	done      chan struct{}

	// tx is the transaction a command should run in instead of opening its own. EXEC sets
	// it so that every queued command shares one lock hold and lands atomically.
	tx *kvs.Tx

	// Transaction state between MULTI and EXEC. Each WATCH adds a handle the store marks
	// when one of its keys changes.
	inMulti    bool
	queued     [][][]byte
	queueError bool
	watches    []*kvs.Watch

	// Subscriptions held by this connection.
	channels map[string]struct{}
	patterns map[string]struct{}

	// scanAfter remembers, per open cursor, the last key a SCAN call reached.
	scanAfter  map[uint64]string
	scanCursor uint64
}

// read runs fn against the store under a read lock, or inside the ambient transaction when
// one is already held.
func (c *respConn) read(fn func(tx *kvs.ReadTx) error) error {
	if c.tx != nil {
		return fn(&c.tx.ReadTx)
	}

	return c.server.store.Read(fn)
}

// write runs fn against the store under a write lock, or inside the ambient transaction
// when one is already held.
func (c *respConn) write(fn func(tx *kvs.Tx) error) error {
	if c.tx != nil {
		return fn(c.tx)
	}

	return c.server.store.Write(fn)
}

// writeFailure reports a command error to the client. Every error a command closure
// returns describes bad input or a type conflict, so echoing it is safe.
func (c *respConn) writeFailure(err error) error {
	return c.writer.WriteError(err.Error())
}

// writeBulkArray writes an array of bulk strings where a nil element is a null, which is
// how the multi-key lookups report a value they could not find.
func (c *respConn) writeBulkArray(values [][]byte) error {
	if err := c.writer.WriteArrayHeader(len(values)); err != nil {
		return err
	}

	for _, value := range values {
		if err := c.writer.WriteBulk(value); err != nil {
			return err
		}
	}

	return nil
}

func (c *respConn) writeBulkStrings(items ...string) error {
	for _, item := range items {
		if err := c.writer.WriteBulkString(item); err != nil {
			return err
		}
	}

	return nil
}

func (c *respConn) flush() error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	return c.writer.Flush()
}

func (c *respConn) serve() {
	defer func() {
		c.server.broker.dropConn(c)
		c.clearWatches()
		close(c.done)
		_ = c.netConn.Close()
	}()

	go c.pumpPushes()

	for {
		args, err := c.reader.ReadCommand()
		if err != nil {
			c.reportReadError(err)

			return
		}
		if len(args) == 0 {
			continue
		}

		if !c.runCommand(args) {
			_ = c.flush()

			return
		}
	}
}

// runCommand dispatches one command while holding the writer, so that a pushed message
// cannot land in the middle of the reply. It reports whether the session continues.
func (c *respConn) runCommand(args [][]byte) bool {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	err := c.dispatch(args)
	if err == nil && c.subscribed() {
		// A subscribed connection also receives pushed messages, so its reply has to reach
		// the client now rather than sit behind one.
		err = c.writer.Flush()
	}

	return err == nil
}

// pumpPushes writes the pub/sub messages other connections queued for this one.
func (c *respConn) pumpPushes() {
	for {
		select {
		case push := <-c.pushes:
			c.pushBytes.Add(-push.size())

			if !c.writePush(push) {
				_ = c.netConn.Close()

				return
			}
		case <-c.done:
			return
		}
	}
}

func (c *respConn) writePush(push respPush) bool {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	var err error
	if push.pattern == "" {
		if err = c.writer.WriteArrayHeader(3); err == nil {
			err = c.writeBulkStrings("message", push.channel, push.payload)
		}
	} else {
		if err = c.writer.WriteArrayHeader(4); err == nil {
			err = c.writeBulkStrings("pmessage", push.pattern, push.channel, push.payload)
		}
	}
	if err == nil {
		err = c.writer.Flush()
	}

	return err == nil
}

// deliver queues a pushed message. It never blocks: a subscriber that cannot keep up within
// its message and byte budget is dropped instead, which is what bounds the publisher's cost.
func (c *respConn) deliver(push respPush) {
	size := push.size()
	if c.pushBytes.Add(size) > respPushBytes {
		c.pushBytes.Add(-size)
		_ = c.netConn.Close()

		return
	}

	select {
	case c.pushes <- push:
	case <-c.done:
		c.pushBytes.Add(-size)
	default:
		c.pushBytes.Add(-size)
		_ = c.netConn.Close()
	}
}

// reportReadError tells the client about a malformed request before hanging up. A
// desynchronized stream cannot be recovered, which is why Redis also closes here.
func (c *respConn) reportReadError(err error) {
	if !errors.Is(err, resp.ErrProtocol) {
		return
	}

	_ = c.writer.WriteError("ERR Protocol error: " + err.Error())
	_ = c.writer.Flush()
}

func (c *respConn) dispatch(args [][]byte) error {
	name := respUpper(args[0])

	cmd, ok := respCommandFor(name)
	if !ok {
		if c.inMulti {
			c.queueError = true
		}

		return c.writer.WriteError(fmt.Sprintf("ERR unknown command '%s'", args[0]))
	}
	if len(args) < cmd.minArgs || (cmd.maxArgs >= 0 && len(args) > cmd.maxArgs) {
		if c.inMulti {
			c.queueError = true
		}

		return c.wrongArgs(name)
	}
	if !c.authed && !respPreAuthCommands[name] {
		return c.writer.WriteError(respErrNoAuth)
	}
	if c.subscribed() && !respSubscribeCommands[name] {
		return c.writer.WriteError(fmt.Sprintf(
			"ERR Can't execute '%s': only (P|S)SUBSCRIBE / (P|S)UNSUBSCRIBE / PING / QUIT / RESET are allowed",
			strings.ToLower(name),
		))
	}
	if c.inMulti && !respTransactionCommands[name] {
		c.queued = append(c.queued, args)

		return c.writer.WriteSimple("QUEUED")
	}

	return cmd.run(c, args)
}

func (c *respConn) wrongArgs(name string) error {
	return c.writer.WriteError(fmt.Sprintf("ERR wrong number of arguments for '%s' command", strings.ToLower(name)))
}

func (c *respConn) unknownSubcommand(container string, args [][]byte) error {
	sub := ""
	if len(args) > 1 {
		sub = string(args[1])
	}

	return c.writer.WriteError(fmt.Sprintf(
		"ERR Unknown subcommand or wrong number of arguments for '%s'. Try %s HELP.",
		sub, strings.ToUpper(container),
	))
}

// checkPassword compares a client credential in constant time, so that a wrong guess does
// not leak how much of it was right.
func (c *respConn) checkPassword(candidate []byte) bool {
	return subtle.ConstantTimeCompare(candidate, []byte(c.server.password)) == 1
}
