package kvs

import (
	"errors"
	"fmt"
)

// ErrNotLeader is what a write to a clustered node that is not the leader gets back. Match it
// with errors.Is; use errors.As with NotLeaderError to find out where the leader is.
var ErrNotLeader = errors.New("not the leader")

// NotLeaderError carries the address of the node that can take the write, so a client can be
// pointed at it rather than told to guess.
type NotLeaderError struct {
	// Leader is empty while an election is in progress and nobody knows yet.
	Leader string
}

func (e *NotLeaderError) Error() string {
	if e.Leader == "" {
		return "not the leader, and no leader is elected yet"
	}

	return fmt.Sprintf("not the leader, %s is", e.Leader)
}

func (e *NotLeaderError) Is(target error) bool {
	return target == ErrNotLeader
}

// SetReplicator routes writes through replicate instead of straight into the keyspace, which is
// how a clustered node makes every write pass consensus first. Passing nil puts it back.
//
// Set it before anything serves: it is read without the lock a write would take, on the
// understanding that it is wired up once at startup.
func (s *Store) SetReplicator(replicate func(fn func(tx *Tx) error) error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.replicate = replicate
}

func (s *Store) replicator() func(fn func(tx *Tx) error) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.replicate
}

// SetCodec sets how stored values are rendered for the log and for replicas. Open does this
// already; a Store from NewStore needs it before it can replicate anything a StringCodec
// cannot handle.
func (s *Store) SetCodec(codec Codec) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.codec = codec
}

// ReplaceWith throws the keyspace away and rebuilds it from snapshot, which is what a node
// restored from a cluster snapshot needs: the agreed state is the only authority, so whatever the
// node held before is worth keeping only until it arrives.
func (s *Store) ReplaceWith(snapshot [][]byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.writeLocked(func(tx *Tx) error {
		tx.Flush()

		return applyFrame(tx, snapshot)
	})
}

// ApplyReplicated applies one frame the cluster has agreed on. A frame is one transaction on the
// node that took the write, and applying it inside one transaction here is what keeps a MULTI
// atomic on every node.
func (s *Store) ApplyReplicated(lines [][]byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.writeLocked(func(tx *Tx) error {
		return applyFrame(tx, lines)
	})
}

func applyFrame(tx *Tx, lines [][]byte) error {
	for _, line := range lines {
		rec, err := decodeRecord(line)
		if err != nil {
			return err
		}

		if err := tx.restore(&rec); err != nil {
			return err
		}
	}

	return nil
}
