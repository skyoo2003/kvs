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
// how a clustered node makes every write pass consensus first, and reports the revision the write
// committed at. Passing nil puts it back.
//
// Set it before anything serves: it is read without the lock a write would take, on the
// understanding that it is wired up once at startup.
//
// Exported for internal/cluster to reach across the package boundary, and outside the v1
// compatibility promise: see website/content/docs/compatibility.md.
func (s *Store) SetReplicator(replicate func(fn func(tx *Tx) error) (int64, error)) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.replicate = replicate
}

func (s *Store) replicator() func(fn func(tx *Tx) error) (int64, error) {
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
// node held before is worth keeping only until it arrives. That goes for the revision too: the
// node comes back at the one the snapshot was taken at.
//
// Exported for internal/cluster to reach across the package boundary, and outside the v1
// compatibility promise: see website/content/docs/compatibility.md.
func (s *Store) ReplaceWith(snapshot [][]byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.rev = 0

	err := s.writeLocked(func(tx *Tx) error {
		tx.Flush()

		return applyFrame(tx, snapshot, true)
	})

	// The flush counts as a change, but an empty snapshot is of a store nothing was written to,
	// and has no revision record to say so.
	if len(snapshot) == 0 {
		s.rev = 0
	}

	return err
}

// ApplyReplicated applies one frame the cluster has agreed on, at the revision the cluster gave
// it, and reports the store's revision afterwards. A frame is one transaction on the node that
// took the write, and applying it inside one transaction here is what keeps a MULTI atomic on
// every node.
//
// The revisions written inside the frame are ignored: they are the leader's guess from before the
// cluster agreed, and only rev is the same on every node.
//
// Exported for internal/cluster to reach across the package boundary, and outside the v1
// compatibility promise: see website/content/docs/compatibility.md.
func (s *Store) ApplyReplicated(rev int64, lines [][]byte) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.writeAt(rev, func(tx *Tx) error {
		return applyFrame(tx, lines, false)
	}); err != nil {
		return 0, err
	}

	return s.rev, nil
}

// applyFrame restores every record in lines. keepRevisions keeps the revisions the records carry,
// which is right for a snapshot; without it they take the transaction's.
func applyFrame(tx *Tx, lines [][]byte, keepRevisions bool) error {
	for _, line := range lines {
		rec, err := decodeRecord(line)
		if err != nil {
			return err
		}

		if !keepRevisions {
			rec.Rev, rec.CreateRev = 0, 0
		}

		if err := tx.restore(&rec); err != nil {
			return err
		}
	}

	return nil
}
