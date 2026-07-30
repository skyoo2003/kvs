package server

import (
	"maps"
	"slices"
	"sync"
)

// respPush is one pub/sub message on its way to a subscriber. An empty pattern means the
// subscriber matched the channel by name rather than by pattern.
type respPush struct {
	pattern string
	channel string
	payload string
}

// size is what the message costs in a subscriber's backlog budget.
func (p respPush) size() int64 {
	return int64(len(p.pattern) + len(p.channel) + len(p.payload))
}

// respBroker routes published messages to the connections subscribed to them.
type respBroker struct {
	mu       sync.RWMutex
	channels map[string]map[*respConn]struct{}
	patterns map[string]map[*respConn]struct{}
}

func newRESPBroker() *respBroker {
	return &respBroker{
		channels: make(map[string]map[*respConn]struct{}),
		patterns: make(map[string]map[*respConn]struct{}),
	}
}

func (b *respBroker) subscribe(conn *respConn, name string, byPattern bool) {
	b.mu.Lock()
	defer b.mu.Unlock()

	registry := b.registry(byPattern)
	if registry[name] == nil {
		registry[name] = make(map[*respConn]struct{})
	}
	registry[name][conn] = struct{}{}
}

func (b *respBroker) unsubscribe(conn *respConn, name string, byPattern bool) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.removeLocked(conn, name, byPattern)
}

// dropConn removes every subscription a closing connection held.
func (b *respBroker) dropConn(conn *respConn) {
	b.mu.Lock()
	defer b.mu.Unlock()

	for name := range conn.channels {
		b.removeLocked(conn, name, false)
	}
	for name := range conn.patterns {
		b.removeLocked(conn, name, true)
	}
}

// publish delivers payload and reports how many subscribers received it. A connection
// subscribed both by name and by a matching pattern is counted once per match, as Redis
// counts it.
func (b *respBroker) publish(channel, payload string) int {
	b.mu.RLock()
	targets := make([]*respConn, 0, len(b.channels[channel]))
	pushes := make([]respPush, 0, len(b.channels[channel]))

	for conn := range b.channels[channel] {
		targets = append(targets, conn)
		pushes = append(pushes, respPush{channel: channel, payload: payload})
	}
	for pattern, conns := range b.patterns {
		if !respGlobMatch(pattern, channel) {
			continue
		}
		for conn := range conns {
			targets = append(targets, conn)
			pushes = append(pushes, respPush{pattern: pattern, channel: channel, payload: payload})
		}
	}
	b.mu.RUnlock()

	// Deliver outside the broker lock, and never inline: a queued push cannot block, so a
	// publisher is not exposed to how fast its subscribers read.
	for i, conn := range targets {
		conn.deliver(pushes[i])
	}

	return len(targets)
}

func (b *respBroker) registry(byPattern bool) map[string]map[*respConn]struct{} {
	if byPattern {
		return b.patterns
	}

	return b.channels
}

func (b *respBroker) removeLocked(conn *respConn, name string, byPattern bool) {
	registry := b.registry(byPattern)
	conns := registry[name]
	if conns == nil {
		return
	}

	delete(conns, conn)
	if len(conns) == 0 {
		delete(registry, name)
	}
}

// subscribed reports whether the connection is in subscribe mode, where RESP2 accepts only
// a small set of commands.
func (c *respConn) subscribed() bool {
	return len(c.channels)+len(c.patterns) > 0
}

func (c *respConn) subscriptionCount() int64 {
	return int64(len(c.channels) + len(c.patterns))
}

// cmdSubscribe handles SUBSCRIBE and PSUBSCRIBE, confirming one channel at a time.
func (c *respConn) cmdSubscribe(args [][]byte) error {
	byPattern := respUpper(args[0]) == respCmdPSubscribe
	kind := "subscribe"
	if byPattern {
		kind = "psubscribe"
	}

	for _, raw := range args[1:] {
		name := string(raw)
		if byPattern {
			if c.patterns == nil {
				c.patterns = make(map[string]struct{})
			}
			c.patterns[name] = struct{}{}
		} else {
			if c.channels == nil {
				c.channels = make(map[string]struct{})
			}
			c.channels[name] = struct{}{}
		}
		c.server.broker.subscribe(c, name, byPattern)

		if err := c.writeSubscriptionReply(kind, name); err != nil {
			return err
		}
	}

	return nil
}

// cmdUnsubscribe handles UNSUBSCRIBE and PUNSUBSCRIBE. With no names it drops every
// subscription of that kind.
func (c *respConn) cmdUnsubscribe(args [][]byte) error {
	byPattern := respUpper(args[0]) == respCmdPUnsubscribe
	kind := "unsubscribe"
	held := c.channels
	if byPattern {
		kind, held = "punsubscribe", c.patterns
	}

	names := make([]string, 0, len(args))
	for _, raw := range args[1:] {
		names = append(names, string(raw))
	}
	if len(names) == 0 {
		names = slices.Collect(maps.Keys(held))
	}

	// Redis still answers once when there was nothing to drop, with a null channel name.
	if len(names) == 0 {
		if err := c.writer.WriteArrayHeader(3); err != nil {
			return err
		}
		if err := c.writer.WriteBulkString(kind); err != nil {
			return err
		}
		if err := c.writer.WriteNull(); err != nil {
			return err
		}

		return c.writer.WriteInt(c.subscriptionCount())
	}

	for _, name := range names {
		delete(held, name)
		c.server.broker.unsubscribe(c, name, byPattern)

		if err := c.writeSubscriptionReply(kind, name); err != nil {
			return err
		}
	}

	return nil
}

func (c *respConn) writeSubscriptionReply(kind, name string) error {
	if err := c.writer.WriteArrayHeader(3); err != nil {
		return err
	}
	if err := c.writeBulkStrings(kind, name); err != nil {
		return err
	}

	return c.writer.WriteInt(c.subscriptionCount())
}

func (c *respConn) cmdPublish(args [][]byte) error {
	received := c.server.broker.publish(string(args[1]), string(args[2]))

	return c.writer.WriteInt(int64(received))
}
