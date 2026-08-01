package server

import (
	"strings"
	"testing"
	"time"

	"github.com/skyoo2003/kvs"
)

func TestRESPEvalConvertsReturnValues(t *testing.T) {
	client := newRESPClient(t, kvs.NewStore())

	tests := []struct {
		name   string
		script string
		want   string
	}{
		{name: "a number loses its fraction", script: "return 3.9", want: ":3"},
		{name: "a string is a bulk string", script: "return 'hi'", want: "$2" + respCRLF + "hi"},
		{name: "true is one", script: "return true", want: ":1"},
		{name: "false is a null", script: "return false", want: "$-1"},
		{name: "no return value is a null", script: "", want: "$-1"},
		{
			name:   "a table is an array",
			script: "return {1,'two'}",
			want:   "*2" + respCRLF + ":1" + respCRLF + "$3" + respCRLF + "two",
		},
		{name: "a table stops at its first nil", script: "return {1,nil,3}", want: "*1" + respCRLF + ":1"},
		{name: "an ok field is a status", script: "return redis.status_reply('DONE')", want: "+DONE"},
		{name: "an err field is an error", script: "return redis.error_reply('ERR nope')", want: "-ERR nope"},
		{
			name:   "a nested table nests",
			script: "return {1,{2}}",
			want:   "*2" + respCRLF + ":1" + respCRLF + "*1" + respCRLF + ":2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client.t = t
			client.do(tt.want+respCRLF, respCmdEval, tt.script, "0")
		})
	}
}

func TestRESPEvalBindsKeysAndArgv(t *testing.T) {
	client := newRESPClient(t, kvs.NewStore())

	client.send(respCmdEval, "return {KEYS[1],KEYS[2],ARGV[1]}", "2", "k1", "k2", "a1")

	got := strings.Join(client.readStringArray(), ",")
	if want := "k1,k2,a1"; got != want {
		t.Fatalf("KEYS and ARGV = %q, want %q", got, want)
	}

	// The count is what splits the two, so it has to be checked before a script sees either.
	client.do("-"+respErrNotInteger+respCRLF, respCmdEval, "return 1", "x")
	client.do("-"+respErrNegativeKeys+respCRLF, respCmdEval, "return 1", "-1")
	client.do("-"+respErrTooManyKeys+respCRLF, respCmdEval, "return 1", "2", "only-one")
}

// TestRESPEvalCallsCommands covers the bridge both ways: arguments reach the command, and the
// reply reaches the script as the value Redis promises.
func TestRESPEvalCallsCommands(t *testing.T) {
	client := newRESPClient(t, kvs.NewStore())

	client.do("+OK"+respCRLF, respCmdEval, "return redis.call('SET', KEYS[1], ARGV[1])", "1", "greeting", "hello")
	client.do("$5"+respCRLF+"hello"+respCRLF, "GET", "greeting")

	// A missing key is false, which is how a script tells an absent value from an empty one.
	client.do(":1"+respCRLF, respCmdEval, "if redis.call('GET','absent') == false then return 1 end return 0", "0")

	// An integer reply arrives as a number, and a number argument goes out as its digits.
	client.do(":7"+respCRLF, respCmdEval, "redis.call('SET','n',3) return redis.call('INCRBY','n',4)", "0")

	// An array reply arrives as a table Lua indexes from one.
	client.do("$1"+respCRLF+"b"+respCRLF, respCmdEval,
		"redis.call('RPUSH','list','a','b') return redis.call('LRANGE','list',0,-1)[2]", "0")
}

// TestRESPEvalEncodesJSON covers cjson, which is how a script reads a structured value it
// stored under one key: without it every such script has to parse by hand or move the work out.
func TestRESPEvalEncodesJSON(t *testing.T) {
	client := newRESPClient(t, kvs.NewStore())

	tests := []struct {
		name   string
		script string
		want   string
	}{
		{name: "an object decodes by key", script: "return cjson.decode('{\"a\":\"b\"}').a", want: "$1" + respCRLF + "b"},
		{name: "an array decodes from one", script: "return cjson.decode('[7,8]')[2]", want: ":8"},
		{name: "a nested value decodes", script: "return cjson.decode('{\"x\":{\"y\":[1,2]}}').x.y[2]", want: ":2"},
		{name: "an array encodes", script: "return cjson.encode({1,2})", want: "$5" + respCRLF + "[1,2]"},
		{name: "a keyed table encodes as an object", script: "return cjson.encode({a=1})", want: "$7" + respCRLF + `{"a":1}`},
		{name: "an empty table encodes as an object", script: "return cjson.encode({})", want: "$2" + respCRLF + "{}"},
		{
			name:   "a round trip holds",
			script: "return cjson.encode(cjson.decode('[1,\"a\"]'))",
			want:   "$7" + respCRLF + `[1,"a"]`,
		},
		// A null is not nil, so it neither ends the array it sits in nor drops the key it is under.
		{name: "a null keeps its place", script: "return #cjson.decode('[1,null,3]')", want: ":3"},
		{
			name:   "a null survives a round trip",
			script: "return cjson.encode(cjson.decode('[null]'))",
			want:   "$6" + respCRLF + "[null]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client.t = t
			client.do(tt.want+respCRLF, respCmdEval, tt.script, "0")
		})
	}

	client.t = t

	// The value a script reads is the one a client wrote, which is the whole point of having it.
	client.do("+OK"+respCRLF, "SET", "doc", `{"n":41}`)
	client.do(":42"+respCRLF, respCmdEval, "return cjson.decode(redis.call('GET',KEYS[1])).n + 1", "1", "doc")

	// Bad input and a value with no JSON spelling are script errors, not silent wrong answers.
	for name, script := range map[string]string{
		"invalid json":     "return cjson.decode('{oops')",
		"a function":       "return cjson.encode(cjson.encode)",
		"a table cycle":    "local t = {} t.self = t return cjson.encode(t)",
		"a missing string": "return cjson.decode()",
	} {
		if line := client.readLineFor(respCmdEval, script, "0"); !strings.HasPrefix(line, "-ERR ") {
			t.Fatalf("EVAL with %s = %q, want an error reply", name, line)
		}
	}
}

func TestRESPEvalRunsAsOneStep(t *testing.T) {
	client := newRESPClient(t, kvs.NewStore())

	client.do(":2"+respCRLF, respCmdEval,
		"redis.call('SET','a','1') redis.call('SET','b','2') return redis.call('DBSIZE')", "0")
	client.do(":2"+respCRLF, "DBSIZE")
}

// TestRESPEvalInsideMulti is the case that would deadlock if a script opened a transaction of
// its own: EXEC already holds the store's write lock when it dispatches the queued EVAL.
func TestRESPEvalInsideMulti(t *testing.T) {
	client := newRESPClient(t, kvs.NewStore())

	client.do("+OK"+respCRLF, respCmdMulti)
	client.do("+QUEUED"+respCRLF, respCmdEval, "return redis.call('SET','k','v')", "0")
	client.do("+QUEUED"+respCRLF, respCmdEval, "return redis.call('GET','k')", "0")
	client.do("*2"+respCRLF+"+OK"+respCRLF+"$1"+respCRLF+"v"+respCRLF, respCmdExec)

	// Still usable afterwards, which proves the script put back the state it borrowed.
	client.do("+PONG"+respCRLF, respCmdPing)
	client.do("$1"+respCRLF+"v"+respCRLF, "GET", "k")
}

func TestRESPEvalShaAnswersNoScriptSoClientsFallBack(t *testing.T) {
	client := newRESPClient(t, kvs.NewStore())

	const script = "return 41 + 1"

	hash := respScriptHash(script)

	// The reply a client library keys its EVAL fallback on.
	client.do("-"+respErrNoScript+respCRLF, respCmdEvalSha, hash, "0")

	// EVAL caches on the way through, so the digest works from then on.
	client.do(":42"+respCRLF, respCmdEval, script, "0")
	client.do(":42"+respCRLF, respCmdEvalSha, hash, "0")
	client.do(":42"+respCRLF, respCmdEvalSha, strings.ToUpper(hash), "0")
}

func TestRESPScriptSubcommands(t *testing.T) {
	client := newRESPClient(t, kvs.NewStore())

	const script = "return 1"

	hash := respScriptHash(script)

	client.do("$40"+respCRLF+hash+respCRLF, respCmdScript, "LOAD", script)
	client.do(":1"+respCRLF, respCmdEvalSha, hash, "0")
	client.do("*2"+respCRLF+":1"+respCRLF+":0"+respCRLF, respCmdScript, "EXISTS", hash, strings.Repeat("0", 40))

	client.do("+OK"+respCRLF, respCmdScript, "FLUSH")
	client.do("*1"+respCRLF+":0"+respCRLF, respCmdScript, "EXISTS", hash)
	client.do("-"+respErrNoScript+respCRLF, respCmdEvalSha, hash, "0")

	// A script that does not compile is refused at load rather than at its first run.
	line := client.readLineFor(respCmdScript, "LOAD", "this is not lua")
	if !strings.HasPrefix(line, "-ERR Error compiling script") {
		t.Fatalf("SCRIPT LOAD of a broken script = %q, want a compile error", line)
	}
	if line = client.readLineFor(respCmdScript, "NOPE"); !strings.HasPrefix(line, "-ERR Unknown subcommand") {
		t.Fatalf("SCRIPT NOPE = %q, want an unknown subcommand error", line)
	}

	// A mode FLUSH does not know is refused rather than read as a plain flush.
	if line = client.readLineFor(respCmdScript, "FLUSH", "NOPE"); !strings.HasPrefix(line, "-ERR Unknown subcommand") {
		t.Fatalf("SCRIPT FLUSH NOPE = %q, want an unknown subcommand error", line)
	}
	client.do("+OK"+respCRLF, respCmdScript, "FLUSH", "ASYNC")
	if line = client.readLineFor(respCmdScript, respCmdExists); !strings.HasPrefix(line, "-ERR wrong number") {
		t.Fatalf("SCRIPT EXISTS with no digest = %q, want an arity error", line)
	}
}

// TestRESPEvalKeepsUnrunnableScriptsOutOfTheCache holds EVAL to what SCRIPT LOAD already does.
// EVAL cached whatever it was sent before compiling it, so a script that cannot parse answered
// EXISTS with a 1 and held part of the cache budget until the next flush.
func TestRESPEvalKeepsUnrunnableScriptsOutOfTheCache(t *testing.T) {
	client := newRESPClient(t, kvs.NewStore())

	const broken = "this is not lua"

	if line := client.readLineFor(respCmdEval, broken, "0"); !strings.HasPrefix(line, "-ERR Error compiling script") {
		t.Fatalf("EVAL of a broken script = %q, want a compile error", line)
	}
	client.do("*1"+respCRLF+":0"+respCRLF, respCmdScript, respCmdExists, respScriptHash(broken))

	// A script that parses and then fails at runtime is still cached, the way Redis caches it.
	const failing = "return missing.field"

	if line := client.readLineFor(respCmdEval, failing, "0"); !strings.HasPrefix(line, "-ERR ") {
		t.Fatalf("EVAL of a failing script = %q, want a runtime error", line)
	}
	client.do("*1"+respCRLF+":1"+respCRLF, respCmdScript, respCmdExists, respScriptHash(failing))
}

func TestRESPEvalReportsFailures(t *testing.T) {
	client := newRESPClient(t, kvs.NewStore())

	const wrongArgs = "ERR wrong number of arguments for 'get' command"

	// A failed redis.call ends the script, and its message reaches the client unwrapped.
	client.do("-"+wrongArgs+respCRLF, respCmdEval, "redis.call('GET') return 'unreachable'", "0")

	// redis.pcall hands the same failure back as a table instead, so the script keeps going.
	client.do("$"+itoa(len(wrongArgs))+respCRLF+wrongArgs+respCRLF, respCmdEval, "return redis.pcall('GET').err", "0")

	// A script may reject its own input the same way.
	client.do("-ERR key must not be empty"+respCRLF, respCmdEval,
		"return redis.error_reply('ERR key must not be empty')", "0")

	for name, script := range map[string]string{
		"a syntax error":  "this is not lua",
		"a runtime error": "return missing.field",
		"a bad argument":  "return redis.call('SET', 'k', {})",
	} {
		if line := client.readLineFor(respCmdEval, script, "0"); !strings.HasPrefix(line, "-ERR ") {
			t.Fatalf("EVAL with %s = %q, want an error reply", name, line)
		}
	}
}

func TestRESPEvalRefusesCommandsThatDoNotBelongInAScript(t *testing.T) {
	client := newRESPClient(t, kvs.NewStore())

	commands := []string{
		respCmdMulti, respCmdSubscribe, respCmdEval, respCmdScript, respCmdQuit,
		// CLIENT renames or reports on the connection whoever called EVAL is holding.
		respCmdClient,
	}
	for _, command := range commands {
		want := "-ERR This Redis command is not allowed from script: " + strings.ToLower(command)
		if line := client.readLineFor(respCmdEval, "return redis.call('"+command+"')", "0"); line != want {
			t.Fatalf("redis.call(%q) = %q, want %q", command, line, want)
		}
	}

	// The connection is still in its ordinary state: not subscribed, not in a transaction.
	client.do("+PONG"+respCRLF, respCmdPing)
}

// TestRESPEvalSandboxesTheInterpreter checks the libraries reaching outside the process are
// absent; without that, anyone who can run EVAL reads whatever the server's user can.
func TestRESPEvalSandboxesTheInterpreter(t *testing.T) {
	client := newRESPClient(t, kvs.NewStore())

	for _, name := range []string{"os", "io", "debug", "package", "require", "dofile", "loadfile", "print", "coroutine"} {
		client.do(":1"+respCRLF, respCmdEval, "if "+name+" == nil then return 1 end return 0", "0")
	}

	// What a script does need is still there.
	client.do(":1"+respCRLF, respCmdEval,
		"if string.len('ab') == 2 and math.floor(1.5) == 1 and table.concat({'a'}) == 'a' then return 1 end return 0", "0")
}

func TestRESPEvalStopsARunawayScript(t *testing.T) {
	restore := respScriptTimeout
	respScriptTimeout = 50 * time.Millisecond

	t.Cleanup(func() { respScriptTimeout = restore })

	client := newRESPClient(t, kvs.NewStore())

	line := client.readLineFor(respCmdEval, "while true do end", "0")
	if !strings.HasPrefix(line, "-ERR Script exceeded") {
		t.Fatalf("EVAL of an endless script = %q, want the time limit error", line)
	}

	// A script catching its own errors must not sit inside pcall forever: the deadline has to
	// keep firing until the script is out of the interpreter.
	line = client.readLineFor(respCmdEval, "while true do pcall(function() end) end", "0")
	if !strings.HasPrefix(line, "-ERR ") {
		t.Fatalf("EVAL of an endless script inside pcall = %q, want an error", line)
	}

	// The store's lock was released, so the server still serves everyone else.
	client.do("+PONG"+respCRLF, respCmdPing)
}

func TestRESPScriptCacheStopsAtItsBudget(t *testing.T) {
	scripts := &respScripts{}
	oversized := strings.Repeat("x", respMaxScriptBytes)

	if !scripts.store("a", oversized) {
		t.Fatal("store() of a script at the budget = false, want it cached")
	}
	if scripts.store("b", "return 1") {
		t.Fatal("store() past the budget = true, want it refused")
	}
	if _, ok := scripts.get("b"); ok {
		t.Fatal("get() of a refused script = ok, want it absent")
	}

	// Re-storing what the cache holds costs nothing, so a repeated EVAL cannot walk the budget up.
	if !scripts.store("a", oversized) {
		t.Fatal("store() of a cached script = false, want it accepted")
	}

	scripts.flush()

	if !scripts.store("b", "return 1") {
		t.Fatal("store() after a flush = false, want the budget released")
	}
}

func TestRESPEvalRequiresAuth(t *testing.T) {
	client := newRESPClientWithPassword(t, kvs.NewStore(), "s3cret")

	client.do("-"+respErrNoAuth+respCRLF, respCmdEval, "return 1", "0")
	client.do("+OK"+respCRLF, respCmdAuth, "s3cret")
	client.do(":1"+respCRLF, respCmdEval, "return 1", "0")
}
