package server

import (
	"bytes"

	"github.com/skyoo2003/kvs"
	"github.com/skyoo2003/kvs/pkg/resp"
)

// respPreAuthCommands may run before AUTH succeeds, so that a client can authenticate and
// so that it can still negotiate or hang up.
var respPreAuthCommands = map[string]bool{
	respCmdAuth:  true,
	respCmdHello: true,
	respCmdQuit:  true,
	respCmdReset: true,
}

// respSubscribeCommands are the only commands RESP2 accepts from a connection that is in
// subscribe mode.
var respSubscribeCommands = map[string]bool{
	respCmdPing:         true,
	respCmdPSubscribe:   true,
	respCmdPUnsubscribe: true,
	respCmdQuit:         true,
	respCmdReset:        true,
	respCmdSubscribe:    true,
	respCmdUnsubscribe:  true,
}

// respTransactionCommands act on the transaction itself rather than being queued into it.
var respTransactionCommands = map[string]bool{
	respCmdDiscard: true,
	respCmdExec:    true,
	respCmdMulti:   true,
	"QUIT":         true,
	"RESET":        true,
	respCmdWatch:   true,
}

func (c *respConn) cmdMulti(_ [][]byte) error {
	if c.inMulti {
		return c.writer.WriteError("ERR MULTI calls can not be nested")
	}

	c.inMulti = true
	c.queued = nil
	c.queueError = false

	return c.writer.WriteSimple(respOK)
}

func (c *respConn) cmdDiscard(_ [][]byte) error {
	if !c.inMulti {
		return c.writer.WriteError("ERR DISCARD without MULTI")
	}

	c.resetTransaction()

	return c.writer.WriteSimple(respOK)
}

// cmdExec runs every queued command inside a single write transaction, so the batch either
// lands whole or not at all.
//
// The replies are encoded into memory rather than into the connection while the store lock is
// held. A reply larger than the connection's buffer would otherwise reach the socket under
// that lock, and one slow client would stall every other writer.
func (c *respConn) cmdExec(_ [][]byte) error {
	if !c.inMulti {
		return c.writer.WriteError("ERR EXEC without MULTI")
	}

	queued, aborted, watches := c.queued, c.queueError, c.watches
	c.inMulti, c.queued, c.queueError, c.watches = false, nil, false, nil

	// Closing happens after Write returns, because Close takes the store lock itself.
	defer func() {
		for _, watch := range watches {
			watch.Close()
		}
	}()

	if aborted {
		return c.writer.WriteError("EXECABORT Transaction discarded because of previous errors.")
	}

	var encoded bytes.Buffer
	buffered := resp.NewWriter(&encoded)
	outer := c.writer

	conflicted := false
	err := c.server.store.Write(func(tx *kvs.Tx) error {
		// Check the watches inside the same lock hold as the batch, or a conflicting write
		// could still slip in between the check and the first queued command.
		for _, watch := range watches {
			if watch.Conflicted() {
				conflicted = true

				return nil
			}
		}

		c.writer, c.tx = buffered, tx
		defer func() { c.writer, c.tx = outer, nil }()

		if err := c.writer.WriteArrayHeader(len(queued)); err != nil {
			return err
		}
		for _, args := range queued {
			if err := c.dispatch(args); err != nil {
				return err
			}
		}

		return buffered.Flush()
	})
	if err != nil {
		return err
	}

	if conflicted {
		return c.writer.WriteNullArray()
	}

	return c.writer.WriteRaw(encoded.Bytes())
}

// cmdWatch arms the optimistic check EXEC makes. Only the named keys count, so an unrelated
// write no longer forces a retry.
func (c *respConn) cmdWatch(args [][]byte) error {
	if c.inMulti {
		return c.writer.WriteError("ERR WATCH inside MULTI is not allowed")
	}

	keys := make([]string, 0, len(args)-1)
	for _, key := range args[1:] {
		keys = append(keys, string(key))
	}
	c.watches = append(c.watches, c.server.store.Watch(keys...))

	return c.writer.WriteSimple(respOK)
}

func (c *respConn) cmdUnwatch(_ [][]byte) error {
	c.clearWatches()

	return c.writer.WriteSimple(respOK)
}

// cmdReset returns the connection to its initial state, which is how a client recovers from
// a half-finished transaction or from subscribe mode.
func (c *respConn) cmdReset(_ [][]byte) error {
	c.resetTransaction()
	c.server.broker.dropConn(c)
	c.channels, c.patterns = nil, nil
	c.name = ""
	c.authed = c.server.password == ""

	return c.writer.WriteSimple("RESET")
}

func (c *respConn) resetTransaction() {
	c.inMulti = false
	c.queued = nil
	c.queueError = false
	c.clearWatches()
}

// clearWatches releases the store's tracking for this connection.
func (c *respConn) clearWatches() {
	for _, watch := range c.watches {
		watch.Close()
	}

	c.watches = nil
}

// cmdAuth checks a client credential. Redis accepts both the bare password form and the
// username form, where the only built-in user is "default".
func (c *respConn) cmdAuth(args [][]byte) error {
	if c.server.password == "" {
		return c.writer.WriteError(
			"ERR Client sent AUTH, but no password is set. Did you mean AUTH <username> <password>?",
		)
	}

	password := args[1]
	if len(args) == 3 {
		if string(args[1]) != respDefaultUser {
			return c.writer.WriteError(respErrWrongPass)
		}
		password = args[2]
	}

	if !c.checkPassword(password) {
		return c.writer.WriteError(respErrWrongPass)
	}

	c.authed = true

	return c.writer.WriteSimple(respOK)
}
