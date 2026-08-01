package server

import (
	"strings"
	"sync"
	"testing"

	"github.com/skyoo2003/kvs"
)

func TestRESPTransactionQueuesAndExecutes(t *testing.T) {
	client := newRESPClient(t, kvs.NewStore())

	client.do("+OK"+respCRLF, "MULTI")
	client.do("+QUEUED"+respCRLF, "SET", "k", "v")
	client.do("+QUEUED"+respCRLF, "INCR", "n")
	client.do("+QUEUED"+respCRLF, "GET", "k")

	// Nothing has run yet.
	client.do("+QUEUED"+respCRLF, "DBSIZE")

	// Replies arrive in order, as one array.
	client.do("*4"+respCRLF+"+OK"+respCRLF+":1"+respCRLF+"$1"+respCRLF+"v"+respCRLF+":2"+respCRLF, "EXEC")
	client.do("$1"+respCRLF+"v"+respCRLF, "GET", "k")
}

func TestRESPTransactionDiscard(t *testing.T) {
	client := newRESPClient(t, kvs.NewStore())

	client.do("+OK"+respCRLF, "MULTI")
	client.do("+QUEUED"+respCRLF, "SET", "k", "v")
	client.do("+OK"+respCRLF, "DISCARD")
	client.do(":0"+respCRLF, "EXISTS", "k")

	client.do("-ERR DISCARD without MULTI"+respCRLF, "DISCARD")
	client.do("-ERR EXEC without MULTI"+respCRLF, "EXEC")
	client.do("+OK"+respCRLF, "MULTI")
	client.do("-ERR MULTI calls can not be nested"+respCRLF, "MULTI")
	client.do("+OK"+respCRLF, "DISCARD")
}

// TestRESPTransactionAbortsAfterQueueError covers the EXECABORT path: a command that could
// not be queued poisons the whole transaction rather than being silently skipped.
func TestRESPTransactionAbortsAfterQueueError(t *testing.T) {
	client := newRESPClient(t, kvs.NewStore())

	client.do("+OK"+respCRLF, "MULTI")
	client.do("+QUEUED"+respCRLF, "SET", "k", "v")
	client.do("-ERR unknown command 'NOSUCHCOMMAND'"+respCRLF, "NOSUCHCOMMAND")
	client.do("-EXECABORT Transaction discarded because of previous errors."+respCRLF, "EXEC")
	client.do(":0"+respCRLF, "EXISTS", "k")

	// An arity error poisons it the same way.
	client.do("+OK"+respCRLF, "MULTI")
	client.do("-ERR wrong number of arguments for 'get' command"+respCRLF, "GET")
	client.do("-EXECABORT Transaction discarded because of previous errors."+respCRLF, "EXEC")
}

// TestRESPTransactionRefusesOversizedQueue keeps one client's MULTI from growing without bound.
// The queue retained every queued command's arguments with no ceiling, so a single connection
// could queue until the process ran out of memory, and that takes the HTTP and gRPC servers
// sharing the process down with it.
func TestRESPTransactionRefusesOversizedQueue(t *testing.T) {
	client := newRESPClient(t, kvs.NewStore())

	client.do("+OK"+respCRLF, "MULTI")

	// Queue past any sane ceiling. One command per megabyte keeps the round trips bounded.
	const megabytes = 128
	chunk := strings.Repeat("x", 1<<20)

	refused := false
	for i := range megabytes {
		client.send("SET", "k:"+itoa(i), chunk)

		reply := client.readLine()
		if reply == "+QUEUED" {
			continue
		}
		if !strings.HasPrefix(reply, "-ERR") {
			t.Fatalf("queued command %d reply = %q, want +QUEUED or an error", i, reply)
		}
		refused = true

		break
	}
	if !refused {
		t.Fatalf("one MULTI queued %d MiB without refusal, want a ceiling", megabytes)
	}

	// A refused command has to poison the batch. Running it half-formed would apply some of the
	// writes the client asked for and drop the rest without saying which.
	client.do("-EXECABORT Transaction discarded because of previous errors."+respCRLF, "EXEC")
	client.do("$-1"+respCRLF, "GET", "k:0")
}

// TestRESPTransactionRefusesManyTinyArguments is the hole a per-command overhead left open. The
// budget counted a command's bytes plus one fixed charge, so a command carrying a hundred
// thousand empty arguments cost almost nothing against it while retaining a slice header and an
// allocation for each one. A few of those outweigh the whole 64 MiB ceiling.
func TestRESPTransactionRefusesManyTinyArguments(t *testing.T) {
	client := newRESPClient(t, kvs.NewStore())

	client.do("+OK"+respCRLF, "MULTI")

	// MSET takes any number of pairs, so one command is enough to carry them.
	const arguments = 100_000
	args := make([]string, 0, arguments)
	args = append(args, "MSET")
	for i := range arguments - 1 {
		args = append(args, "k"+itoa(i))
	}

	refused := false
	for range 16 {
		client.send(args...)
		if reply := client.readLine(); reply != "+QUEUED" {
			if !strings.HasPrefix(reply, "-ERR") {
				t.Fatalf("queued command reply = %q, want +QUEUED or an error", reply)
			}
			refused = true

			break
		}
	}
	if !refused {
		t.Fatal("a MULTI queued millions of arguments without refusal, want the ceiling to count them")
	}

	client.do("-EXECABORT Transaction discarded because of previous errors."+respCRLF, "EXEC")
}

// TestRESPTransactionQueueBudgetResets keeps a refused batch from disabling the connection: the
// budget belongs to one transaction, not to the session.
func TestRESPTransactionQueueBudgetResets(t *testing.T) {
	client := newRESPClient(t, kvs.NewStore())

	chunk := strings.Repeat("x", 1<<20)
	client.do("+OK"+respCRLF, "MULTI")
	for i := range 128 {
		client.send("SET", "k:"+itoa(i), chunk)
		if client.readLine() != "+QUEUED" {
			break
		}
	}
	client.do("-EXECABORT Transaction discarded because of previous errors."+respCRLF, "EXEC")

	client.do("+OK"+respCRLF, "MULTI")
	client.do("+QUEUED"+respCRLF, "SET", "after", "v")
	client.do("*1"+respCRLF+"+OK"+respCRLF, "EXEC")
	client.do("$1"+respCRLF+"v"+respCRLF, "GET", "after")
}

// TestRESPTransactionReportsCommandErrorsInline checks that an error raised while running a
// queued command is one element of the reply array, not a failure of the whole EXEC.
func TestRESPTransactionReportsCommandErrorsInline(t *testing.T) {
	client := newRESPClient(t, kvs.NewStore())

	client.do(":1"+respCRLF, "SADD", "s", "m")
	client.do("+OK"+respCRLF, "MULTI")
	client.do("+QUEUED"+respCRLF, "SET", "k", "v")
	client.do("+QUEUED"+respCRLF, "GET", "s")
	client.do("*2"+respCRLF+"+OK"+respCRLF+"-"+respErrWrongType+respCRLF, "EXEC")

	// The command that did succeed still took effect, as Redis does not roll back.
	client.do("$1"+respCRLF+"v"+respCRLF, "GET", "k")
}

func TestRESPWatchAbortsOnConflictingWrite(t *testing.T) {
	store := kvs.NewStore()
	client := newRESPClient(t, store)

	client.do("+OK"+respCRLF, "SET", "k", "1")
	client.do("+OK"+respCRLF, "WATCH", "k")
	client.do("+OK"+respCRLF, "MULTI")
	client.do("+QUEUED"+respCRLF, "SET", "k", "2")

	// Another writer touches the watched key between WATCH and EXEC.
	if err := store.Put("k", "x"); err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	client.do("*-1"+respCRLF, "EXEC")
	client.do("$1"+respCRLF+"x"+respCRLF, "GET", "k")
}

// TestRESPWatchIgnoresUnrelatedWrites covers the point of tracking keys individually: a write
// somewhere else in the keyspace must not force an optimistic retry.
func TestRESPWatchIgnoresUnrelatedWrites(t *testing.T) {
	store := kvs.NewStore()
	client := newRESPClient(t, store)

	client.do("+OK"+respCRLF, "SET", "k", "1")
	client.do("+OK"+respCRLF, "WATCH", "k")
	client.do("+OK"+respCRLF, "MULTI")
	client.do("+QUEUED"+respCRLF, "SET", "k", "2")

	if err := store.Put("unrelated", "x"); err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	client.do("*1"+respCRLF+"+OK"+respCRLF, "EXEC")
	client.do("$1"+respCRLF+"2"+respCRLF, "GET", "k")
}

// TestRESPWatchSeesDeletionOfWatchedKey checks the case a version comparison against the
// final state would miss: the key is created and removed again before EXEC.
func TestRESPWatchSeesDeletionOfWatchedKey(t *testing.T) {
	store := kvs.NewStore()
	client := newRESPClient(t, store)

	client.do("+OK"+respCRLF, "WATCH", "ghost")
	client.do("+OK"+respCRLF, "MULTI")
	client.do("+QUEUED"+respCRLF, "SET", "ghost", "mine")

	if err := store.Put("ghost", "theirs"); err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	if err := store.Delete("ghost"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	client.do("*-1"+respCRLF, "EXEC")
	client.do(":0"+respCRLF, "EXISTS", "ghost")
}

func TestRESPWatchAllowsUncontestedExec(t *testing.T) {
	client := newRESPClient(t, kvs.NewStore())

	client.do("+OK"+respCRLF, "SET", "k", "1")
	client.do("+OK"+respCRLF, "WATCH", "k")
	client.do("+OK"+respCRLF, "MULTI")
	client.do("+QUEUED"+respCRLF, "SET", "k", "2")
	client.do("*1"+respCRLF+"+OK"+respCRLF, "EXEC")
	client.do("$1"+respCRLF+"2"+respCRLF, "GET", "k")

	// UNWATCH drops the check, so a later conflict no longer aborts.
	client.do("+OK"+respCRLF, "WATCH", "k")
	client.do("+OK"+respCRLF, "UNWATCH")
	client.do("+OK"+respCRLF, "MULTI")
	client.do("+QUEUED"+respCRLF, "SET", "k", "3")
	client.do("*1"+respCRLF+"+OK"+respCRLF, "EXEC")

	client.do("+OK"+respCRLF, "MULTI")
	client.do("-ERR WATCH inside MULTI is not allowed"+respCRLF, "WATCH", "k")
	client.do("+QUEUED"+respCRLF, "GET", "k")
	client.do("*1"+respCRLF+"$1"+respCRLF+"3"+respCRLF, "EXEC")
}

// TestRESPTransactionIsAtomic is the check that EXEC really holds one lock for the whole
// batch: a concurrent writer must never observe a half-applied transaction.
func TestRESPTransactionIsAtomic(t *testing.T) {
	const rounds = 200

	store := kvs.NewStore()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()

		for range rounds {
			_ = store.Write(func(tx *kvs.Tx) error {
				tx.Set("a", kvs.Entry{Value: "1"})
				tx.Set("b", kvs.Entry{Value: "1"})

				return nil
			})
			_ = store.Write(func(tx *kvs.Tx) error {
				tx.Set("a", kvs.Entry{Value: "2"})
				tx.Set("b", kvs.Entry{Value: "2"})

				return nil
			})
		}
	}()

	reader := newRESPClient(t, store)
	for range rounds {
		reader.send("MULTI")
		reader.expect("+OK" + respCRLF)
		reader.send("GET", "a")
		reader.expect("+QUEUED" + respCRLF)
		reader.send("GET", "b")
		reader.expect("+QUEUED" + respCRLF)

		reader.send("EXEC")
		reader.expect("*2" + respCRLF)
		first, second := reader.readBulk(), reader.readBulk()
		if first != second {
			t.Fatalf("EXEC read a = %q and b = %q, want one consistent snapshot", first, second)
		}
	}

	wg.Wait()
}

func TestRESPAuthGatesCommands(t *testing.T) {
	client := newRESPClientWithPassword(t, kvs.NewStore(), "s3cret")

	client.do("-"+respErrNoAuth+respCRLF, "GET", "k")
	client.do("-"+respErrWrongPass+respCRLF, "AUTH", "wrong")
	client.do("-"+respErrWrongPass+respCRLF, "AUTH", "nobody", "s3cret")
	client.do("-"+respErrNoAuth+respCRLF, "GET", "k")

	client.do("+OK"+respCRLF, "AUTH", "s3cret")
	client.do("$-1"+respCRLF, "GET", "k")

	// The username form works too, with the one built-in user.
	client.do("+OK"+respCRLF, "AUTH", "default", "s3cret")
}

func TestRESPAuthThroughHello(t *testing.T) {
	client := newRESPClientWithPassword(t, kvs.NewStore(), "s3cret")

	client.do("-"+respErrNoAuth+respCRLF, "HELLO", "2")
	client.do("-"+respErrWrongPass+respCRLF, "HELLO", "2", "AUTH", "default", "nope")

	client.send("HELLO", "2", "AUTH", "default", "s3cret", "SETNAME", "prober")
	client.expect("*14" + respCRLF)
	for range 13 {
		client.readBulk()
	}
	client.expect("*0" + respCRLF)

	client.do("$-1"+respCRLF, "GET", "k")
	client.do("$6"+respCRLF+"prober"+respCRLF, "CLIENT", "GETNAME")
}

// TestRESPHelloAppliesNothingOnLaterSyntaxError holds the AUTH clause to the same rule SETNAME
// already follows. AUTH took effect the moment it parsed, so a HELLO rejected by a later clause
// answered an error on a connection it had quietly authenticated.
func TestRESPHelloAppliesNothingOnLaterSyntaxError(t *testing.T) {
	client := newRESPClientWithPassword(t, kvs.NewStore(), "s3cret")

	client.do("-"+respErrSyntax+respCRLF, "HELLO", "2", "AUTH", "default", "s3cret", "GARBAGE")
	client.do("-"+respErrNoAuth+respCRLF, "GET", "k")

	// A handshake that parses whole still authenticates.
	client.send("HELLO", "2", "AUTH", "default", "s3cret")
	client.expect("*14" + respCRLF)
	for range 13 {
		client.readBulk()
	}
	client.expect("*0" + respCRLF)
	client.do("$-1"+respCRLF, "GET", "k")
}

func TestRESPAuthWithoutPasswordConfigured(t *testing.T) {
	client := newRESPClient(t, kvs.NewStore())

	client.send("AUTH", "anything")
	if line := client.readLine(); !strings.HasPrefix(line, "-ERR Client sent AUTH, but no password is set") {
		t.Fatalf("AUTH reply = %q, want the no-password error", line)
	}

	// HELLO carries the same clause and used to answer WRONGPASS here, which sent operators
	// looking for a credential mismatch on a server that has no credential at all.
	client.do("-"+errRESPNoPassword.Error()+respCRLF, "HELLO", "2", "SETNAME", "prober", "AUTH", "default", "pw")
	// The clause that ran before the failure must not have taken effect either.
	client.do("$-1"+respCRLF, "CLIENT", "GETNAME")
}

func TestRESPResetClearsConnectionState(t *testing.T) {
	client := newRESPClient(t, kvs.NewStore())

	client.do("+OK"+respCRLF, "CLIENT", "SETNAME", "prober")
	client.do("+OK"+respCRLF, "MULTI")
	client.do("+QUEUED"+respCRLF, "SET", "k", "v")
	client.do("+RESET"+respCRLF, "RESET")

	client.do("-ERR EXEC without MULTI"+respCRLF, "EXEC")
	client.do(":0"+respCRLF, "EXISTS", "k")
	client.do("$-1"+respCRLF, "CLIENT", "GETNAME")
}

// TestRESPWatchIgnoresNoOpCollectionWrite covers the other half of tracking keys precisely: a
// command that changed nothing must not abort someone else's transaction. Storing the container
// back unconditionally made "SADD s m" for a member the set already held look like a write.
func TestRESPWatchIgnoresNoOpCollectionWrite(t *testing.T) {
	clients := newRESPClients(t, 2)
	watcher, other := clients[0], clients[1]

	watcher.do(":1"+respCRLF, "SADD", "s", "m")
	watcher.do("+OK"+respCRLF, "WATCH", "s")
	watcher.do("+OK"+respCRLF, "MULTI")
	watcher.do("+QUEUED"+respCRLF, "SET", "out", "1")

	// None of these change anything, so none of them may conflict.
	other.do(":0"+respCRLF, "SADD", "s", "m")
	other.do(":0"+respCRLF, "SREM", "s", "absent")

	watcher.do("*1"+respCRLF+"+OK"+respCRLF, "EXEC")
}

// TestRESPWatchDeduplicatesKeys keeps a client from growing the store's watcher table without
// bound: every registration is walked under the write lock on each change to that key.
func TestRESPWatchDeduplicatesKeys(t *testing.T) {
	store := kvs.NewStore()
	client := newRESPClient(t, store)

	for range 5 {
		client.do("+OK"+respCRLF, "WATCH", "k", "k")
	}

	client.do("+OK"+respCRLF, "MULTI")
	client.do("+QUEUED"+respCRLF, "SET", "k", "2")
	client.do("*1"+respCRLF+"+OK"+respCRLF, "EXEC")

	// EXEC released the handles, so nothing is left tracking the key.
	client.do("+OK"+respCRLF, "WATCH", "k")
	client.do("+OK"+respCRLF, "UNWATCH")
}
