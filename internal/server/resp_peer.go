package server

import (
	"bufio"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/skyoo2003/kvs/pkg/resp"
)

// respPeerTimeout bounds a conversation with another node. Nothing here waits on a person.
const respPeerTimeout = 10 * time.Second

// peerConn talks to another kvs node over RESP. pkg/resp reads client commands, which is the
// other half of the conversation, so a client reading replies needs this much of its own.
type peerConn struct {
	conn   net.Conn
	reader *bufio.Reader
	writer *resp.Writer
}

func newPeerConn(conn net.Conn) *peerConn {
	return &peerConn{conn: conn, reader: bufio.NewReader(conn), writer: resp.NewWriter(conn)}
}

// send writes one command as a RESP array of bulk strings.
//
// The command name is a parameter of its own rather than the first of args, so that a failure can
// name the command with no way to reach an argument: one of the arguments is a password, and the
// error this returns is logged. Holding both in one slice made that safe only for as long as
// every caller happened to put something harmless first.
func (p *peerConn) send(cmd string, args ...string) error {
	if err := p.writer.WriteArrayHeader(len(args) + 1); err != nil {
		return fmt.Errorf("send %s: %w", cmd, err)
	}
	if err := p.writer.WriteBulkString(cmd); err != nil {
		return fmt.Errorf("send %s: %w", cmd, err)
	}
	for _, arg := range args {
		if err := p.writer.WriteBulkString(arg); err != nil {
			return fmt.Errorf("send %s: %w", cmd, err)
		}
	}
	if err := p.writer.Flush(); err != nil {
		return fmt.Errorf("send %s: %w", cmd, err)
	}

	return nil
}

// expectOK reads one reply and treats anything but a simple string as a refusal. The peer is
// another process on the network, so its answer is input rather than a promise: the deadline
// and the buffered reader's own line limit are what bound it.
func (p *peerConn) expectOK(what string) error {
	if err := p.conn.SetReadDeadline(time.Now().Add(respPeerTimeout)); err != nil {
		return fmt.Errorf("set %s deadline: %w", what, err)
	}

	line, err := p.reader.ReadSlice('\n')
	if err != nil {
		return fmt.Errorf("read %s reply: %w", what, err)
	}

	reply := strings.TrimRight(string(line), "\r\n")
	if !strings.HasPrefix(reply, "+") {
		return fmt.Errorf("%s refused: %s", what, strings.TrimPrefix(reply, "-"))
	}

	return nil
}
