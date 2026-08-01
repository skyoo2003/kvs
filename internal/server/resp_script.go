package server

import (
	"bytes"
	"context"
	"crypto/sha1" //nolint:gosec // SHA1 names a cache entry here; the EVALSHA protocol specifies it.
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	lua "github.com/yuin/gopher-lua"

	"github.com/skyoo2003/kvs"
	"github.com/skyoo2003/kvs/pkg/resp"
)

const (
	respCmdEval    = "EVAL"
	respCmdEvalSha = "EVALSHA"
	respCmdScript  = "SCRIPT"

	// respMaxScriptBytes bounds the script cache, the way respMaxQueuedBytes bounds a transaction
	// queue: EVAL caches whatever a client sends. Past it EVAL still runs the script and only
	// skips caching, so the client loses the EVALSHA shortcut rather than the command.
	respMaxScriptBytes = 16 * 1024 * 1024

	// respErrNoScript is what every client library keys its EVALSHA-then-EVAL fallback on.
	// Answering anything else, "unknown command" included, leaves that fallback unreachable.
	respErrNoScript = "NOSCRIPT No matching script. Please use EVAL."

	respErrScriptBudget  = "ERR SCRIPT LOAD exceeds the script cache budget"
	respErrNegativeKeys  = "ERR Number of keys can't be negative"
	respErrTooManyKeys   = "ERR Number of keys can't be greater than number of args"
	respErrScriptArgType = "ERR Lua redis lib command arguments must be strings or integers"
	respErrScriptNoArgs  = "ERR Please specify at least one argument for this redis lib call"

	// The two table fields Redis gives a meaning to in a reply.
	respLuaErrField = "err"
	respLuaOKField  = "ok"

	// respLuaMaxDepth bounds how deep a returned table is walked, so a table holding itself
	// cannot recurse until the stack gives out and takes the process with it.
	respLuaMaxDepth = 32
)

// respScriptTimeout bounds one script's run. A script holds the store's write lock start to
// finish, so an endless loop would stop every other client for the life of the process. Redis
// answers that with SCRIPT KILL, which needs a second connection served while the first is
// still in the interpreter, so the bound here has to be a deadline the interpreter honors.
// A variable rather than a constant so its test need not take five seconds.
var respScriptTimeout = 5 * time.Second

// respScripts is the cache EVALSHA looks a script up in. It is per server, not per connection:
// client libraries pool sockets, so the one that loaded a script rarely runs it next.
type respScripts struct {
	mu     sync.Mutex
	bodies map[string]string
	bytes  int
}

// store caches body under hash, reporting whether it fit. Callers may ignore the answer: an
// uncached script still runs, it just cannot be reached by digest.
func (s *respScripts) store(hash, body string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.bodies[hash]; ok {
		return true
	}
	if s.bytes+len(body) > respMaxScriptBytes {
		return false
	}
	if s.bodies == nil {
		s.bodies = make(map[string]string)
	}

	s.bodies[hash] = body
	s.bytes += len(body)

	return true
}

func (s *respScripts) get(hash string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	body, ok := s.bodies[hash]

	return body, ok
}

func (s *respScripts) flush() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.bodies, s.bytes = nil, 0
}

// respScriptDenied are the commands redis.call refuses: the transaction family, because a
// script is already one atomic step; the subscribe and session families, which would change a
// connection out from under its owner; and the scripting commands, which would recurse or flush
// the cache being run from. SELECT stays, as in Redis: kvs has one keyspace to select.
var respScriptDenied = map[string]bool{
	respCmdMulti:        true,
	respCmdExec:         true,
	respCmdDiscard:      true,
	respCmdWatch:        true,
	respCmdUnwatch:      true,
	respCmdSubscribe:    true,
	respCmdPSubscribe:   true,
	respCmdUnsubscribe:  true,
	respCmdPUnsubscribe: true,
	respCmdEval:         true,
	respCmdEvalSha:      true,
	respCmdScript:       true,
	respCmdAuth:         true,
	respCmdClient:       true,
	respCmdHello:        true,
	respCmdQuit:         true,
	respCmdReset:        true,
}

// respScriptCommands holds the Lua scripting commands.
func respScriptCommands() map[string]respCommand {
	return map[string]respCommand{
		respCmdEval:    {run: (*respConn).cmdEval, minArgs: 3, maxArgs: -1},
		respCmdEvalSha: {run: (*respConn).cmdEvalSha, minArgs: 3, maxArgs: -1},
		respCmdScript:  {run: (*respConn).cmdScript, minArgs: 2, maxArgs: -1},
	}
}

// cmdEval runs a script sent in full, caching it so the next call can send the digest.
func (c *respConn) cmdEval(args [][]byte) error {
	return c.runScript(string(args[1]), args, true)
}

// cmdEvalSha runs a script the cache already holds.
func (c *respConn) cmdEvalSha(args [][]byte) error {
	body, ok := c.server.scripts.get(strings.ToLower(string(args[1])))
	if !ok {
		return c.writer.WriteError(respErrNoScript)
	}

	return c.runScript(body, args, false)
}

func (c *respConn) cmdScript(args [][]byte) error {
	switch respUpper(args[1]) {
	case "LOAD":
		if len(args) != 3 {
			return c.unknownSubcommand(respCmdScript, args)
		}

		return c.scriptLoad(string(args[2]))
	case respCmdExists:
		if len(args) < 3 {
			return c.wrongArgs("script|exists")
		}

		return c.scriptExists(args[2:])
	case "FLUSH":
		// ASYNC and SYNC only say how Redis frees the memory, so both do the same thing here.
		// Anything else is refused rather than ignored, so a typo is not read as a flush.
		if len(args) > 3 || (len(args) == 3 && !isFlushMode(string(args[2]))) {
			return c.unknownSubcommand(respCmdScript, args)
		}
		c.server.scripts.flush()

		return c.writer.WriteSimple(respOK)
	default:
		return c.unknownSubcommand(respCmdScript, args)
	}
}

// scriptLoad compiles before caching, so a syntax error surfaces at load, not at first run.
func (c *respConn) scriptLoad(body string) error {
	if err := respCompileScript(body); err != nil {
		return c.writer.WriteError("ERR Error compiling script: " + err.Error())
	}

	hash := respScriptHash(body)
	if !c.server.scripts.store(hash, body) {
		return c.writer.WriteError(respErrScriptBudget)
	}

	return c.writer.WriteBulkString(hash)
}

func (c *respConn) scriptExists(hashes [][]byte) error {
	if err := c.writer.WriteArrayHeader(len(hashes)); err != nil {
		return err
	}

	for _, raw := range hashes {
		_, known := c.server.scripts.get(strings.ToLower(string(raw)))
		if err := c.writer.WriteInt(boolToInt(known)); err != nil {
			return err
		}
	}

	return nil
}

// runScript executes body with KEYS and ARGV bound, inside one write transaction, so every
// redis.call it makes lands together. cache asks for the body to be remembered under its
// digest, which EVAL wants and EVALSHA, having come from the cache, does not.
//
// The reply is encoded into memory under the lock and spliced in after, for the same reason
// EXEC does it: a large reply reaching the socket under the lock stalls every other writer.
func (c *respConn) runScript(body string, args [][]byte, cache bool) error {
	keys, argv, err := respScriptArgs(args)
	if err != nil {
		return c.writer.WriteError(err.Error())
	}

	// ponytail: a fresh interpreter per call. A pooled one carries the last script's globals
	// into the next, and Redis promises a script that cannot see what ran before it.
	state := lua.NewState(lua.Options{SkipOpenLibs: true})
	defer state.Close()

	// Neither compiling nor building the sandbox touches the store, so both happen before the
	// lock. Compiling first also keeps an unparseable script out of the cache, where SCRIPT
	// EXISTS would call it runnable and it would hold budget until the next flush.
	compiled, compileErr := state.LoadString(body)
	if compileErr != nil {
		return c.writer.WriteError("ERR Error compiling script: " + compileErr.Error())
	}
	if cache {
		c.server.scripts.store(respScriptHash(body), body)
	}
	c.openScriptAPI(state, keys, argv)

	var encoded bytes.Buffer
	buffered := resp.NewWriter(&encoded)
	outerWriter, outerTx := c.writer, c.tx

	var failure, expired error

	err = c.write(func(tx *kvs.Tx) error {
		// The deadline starts here, not at the top: a script queued behind another writer would
		// otherwise spend its budget waiting for the lock and be stopped without having run.
		ctx, cancel := context.WithTimeout(context.Background(), respScriptTimeout)
		defer cancel()
		state.SetContext(ctx)

		// redis.call re-enters the same dispatch table a client's command does, so the connection
		// has to look like one inside a transaction for the length of the script. Restoring what
		// was there rather than clearing it is what keeps EVAL working under EXEC.
		c.writer, c.tx = buffered, tx
		defer func() { c.writer, c.tx = outerWriter, outerTx }()

		state.Push(compiled)
		if failure = state.PCall(0, lua.MultRet, nil); failure != nil {
			// Read before the deferred cancel above turns this into a plain cancellation.
			expired = ctx.Err()

			return nil
		}
		if writeErr := respWriteLuaReply(buffered, state.Get(1), 0); writeErr != nil {
			return writeErr
		}

		return buffered.Flush()
	})
	if err != nil {
		return err
	}

	if failure != nil {
		return c.writer.WriteError(respScriptFailure(failure, expired))
	}

	return c.writer.WriteRaw(encoded.Bytes())
}

// respScriptArgs splits an EVAL argument list into keys and arguments. Redis makes the caller
// count the keys, and checking it here fails a bad count at the call site, not mid-script.
func respScriptArgs(args [][]byte) (keys, argv [][]byte, err error) {
	count, convErr := strconv.Atoi(string(args[2]))
	if convErr != nil {
		return nil, nil, errRESPNotInteger
	}
	if count < 0 {
		return nil, nil, errors.New(respErrNegativeKeys)
	}
	if count > len(args)-3 {
		return nil, nil, errors.New(respErrTooManyKeys)
	}

	return args[3 : 3+count], args[3+count:], nil
}

// openScriptAPI builds the sandbox: the libraries that cannot reach outside the process, the
// KEYS and ARGV tables, and the redis table.
//
// What is left out is the point. os and io reach the filesystem, package and require load code
// from disk, and debug reaches another frame's locals; without that, anyone who can run EVAL
// can read whatever the server's user can.
func (c *respConn) openScriptAPI(state *lua.LState, keys, argv [][]byte) {
	for _, lib := range []struct {
		name string
		open lua.LGFunction
	}{
		{lua.BaseLibName, lua.OpenBase},
		{lua.TabLibName, lua.OpenTable},
		{lua.StringLibName, lua.OpenString},
		{lua.MathLibName, lua.OpenMath},
	} {
		state.Push(state.NewFunction(lib.open))
		state.Push(lua.LString(lib.name))
		state.Call(1, 0)
	}

	// The base library arrives whole, so its filesystem and stdout parts have to be taken back.
	for _, name := range []string{"dofile", "loadfile", "print", "module", "require", "newproxy", "collectgarbage"} {
		state.SetGlobal(name, lua.LNil)
	}

	state.SetGlobal("KEYS", respLuaStrings(state, keys))
	state.SetGlobal("ARGV", respLuaStrings(state, argv))
	// cjson stays in reach of the process only, so opening it costs the sandbox nothing, and
	// without it every script that stores a structured value has to hand-roll a parser.
	state.SetGlobal("cjson", respCJSONTable(state))
	state.SetGlobal("redis", c.newRedisTable(state))
	// Valkey renamed the table and kept the old name working, so both names reach it.
	state.SetGlobal("server", state.GetGlobal("redis"))
}

// newRedisTable builds the redis table a script calls into.
func (c *respConn) newRedisTable(state *lua.LState) *lua.LTable {
	// One scratch buffer for the whole script: each reply is parsed before the next call runs.
	var scratch bytes.Buffer
	writer := resp.NewWriter(&scratch)

	table := state.NewTable()
	state.SetFuncs(table, map[string]lua.LGFunction{
		"call":         c.scriptCallFunc(writer, &scratch, true),
		"pcall":        c.scriptCallFunc(writer, &scratch, false),
		"error_reply":  respLuaReplyField(respLuaErrField),
		"status_reply": respLuaReplyField(respLuaOKField),
		"sha1hex":      respLuaSHA1Hex,
		// kvs has no script log, and a script calling it wants to keep running, so it is dropped.
		"log": func(*lua.LState) int { return 0 },
	})

	return table
}

// scriptCallFunc builds redis.call and redis.pcall, which differ only in what they do with a
// failure: call raises it and ends the script, pcall hands it back as a table.
func (c *respConn) scriptCallFunc(writer *resp.Writer, scratch *bytes.Buffer, raise bool) lua.LGFunction {
	return func(state *lua.LState) int {
		reply, err := c.scriptCall(state, writer, scratch)
		if err == nil {
			// An error reply is a value to the parser but a failure to the script.
			if replyErr, isErr := reply.(resp.Error); isErr {
				err = errors.New(string(replyErr))
			}
		}
		if err != nil {
			failure := state.NewTable()
			failure.RawSetString(respLuaErrField, lua.LString(err.Error()))
			if raise {
				state.Error(failure, 1)

				return 0
			}

			state.Push(failure)

			return 1
		}

		state.Push(respReplyToLua(state, reply, 0))

		return 1
	}
}

// scriptCall runs one redis.call through the same dispatch table a client's command uses, so a
// command behaves the same from either, and later ones need no second table to maintain.
func (c *respConn) scriptCall(state *lua.LState, writer *resp.Writer, scratch *bytes.Buffer) (any, error) {
	args, err := respScriptCallArgs(state)
	if err != nil {
		return nil, err
	}

	name := respUpper(args[0])
	if respScriptDenied[name] {
		return nil, fmt.Errorf("ERR This Redis command is not allowed from script: %s", strings.ToLower(name))
	}

	scratch.Reset()

	outer := c.writer
	c.writer = writer
	err = c.dispatch(args)
	c.writer = outer

	// Flush even on failure: scratch.Reset does not reach the writer's own buffer, so bytes left
	// there would prefix the next call's reply with the tail of a command already run.
	flushErr := writer.Flush()
	if err != nil {
		return nil, err
	}
	if flushErr != nil {
		return nil, flushErr
	}

	reply, rest, err := resp.ParseReply(scratch.Bytes())
	if err != nil {
		return nil, err
	}
	if len(rest) != 0 {
		return nil, fmt.Errorf("ERR %s answered a script with more than one reply", strings.ToLower(name))
	}

	return reply, nil
}

// respScriptCallArgs reads a redis.call argument list off the Lua stack. Strings and numbers
// only, as in Redis: anything else has no one obvious spelling on the wire.
func respScriptCallArgs(state *lua.LState) ([][]byte, error) {
	top := state.GetTop()
	if top == 0 {
		return nil, errors.New(respErrScriptNoArgs)
	}

	args := make([][]byte, 0, top)
	for at := 1; at <= top; at++ {
		switch value := state.Get(at).(type) {
		case lua.LString:
			args = append(args, []byte(value))
		case lua.LNumber:
			args = append(args, []byte(respFormatFloat(float64(value))))
		default:
			return nil, errors.New(respErrScriptArgType)
		}
	}

	return args, nil
}

// respReplyToLua converts a reply into the Lua value Redis documents. A missing value is false
// rather than nil, because a nil inside a Lua table ends the table.
func respReplyToLua(state *lua.LState, reply any, depth int) lua.LValue {
	if depth > respLuaMaxDepth {
		return lua.LFalse
	}

	switch value := reply.(type) {
	case resp.Status:
		return respLuaField(state, respLuaOKField, string(value))
	case resp.Error:
		return respLuaField(state, respLuaErrField, string(value))
	case int64:
		return lua.LNumber(value)
	case []byte:
		if value == nil {
			return lua.LFalse
		}

		return lua.LString(value)
	case []any:
		if value == nil {
			return lua.LFalse
		}

		table := state.NewTable()
		for _, item := range value {
			table.Append(respReplyToLua(state, item, depth+1))
		}

		return table
	default:
		return lua.LFalse
	}
}

// respWriteLuaReply converts a script's return value into a reply as Redis documents it: true
// is 1, false is a null, a number loses its fraction, and a table ends at its first nil.
func respWriteLuaReply(writer *resp.Writer, value lua.LValue, depth int) error {
	if depth > respLuaMaxDepth {
		return writer.WriteNull()
	}

	switch typed := value.(type) {
	case lua.LBool:
		if !bool(typed) {
			return writer.WriteNull()
		}

		return writer.WriteInt(1)
	case lua.LNumber:
		return writer.WriteInt(int64(typed))
	case lua.LString:
		return writer.WriteBulkString(string(typed))
	case *lua.LTable:
		return respWriteLuaTable(writer, typed, depth)
	default:
		// Nil, and everything else with no reply that means it, such as a function.
		return writer.WriteNull()
	}
}

func respWriteLuaTable(writer *resp.Writer, table *lua.LTable, depth int) error {
	if field, ok := table.RawGetString(respLuaErrField).(lua.LString); ok {
		return writer.WriteError(string(field))
	}
	if field, ok := table.RawGetString(respLuaOKField).(lua.LString); ok {
		return writer.WriteSimple(string(field))
	}

	// The count has to be known before the header, and a Lua array ends at its first nil.
	size := 0
	for table.RawGetInt(size+1) != lua.LNil {
		size++
	}

	if err := writer.WriteArrayHeader(size); err != nil {
		return err
	}
	for at := 1; at <= size; at++ {
		if err := respWriteLuaReply(writer, table.RawGetInt(at), depth+1); err != nil {
			return err
		}
	}

	return nil
}

// respScriptFailure renders what the interpreter reported. timeout wins, because the
// interpreter reports a canceled context as a Lua error whose text tells a client nothing. An
// err table is a message the script chose, redis.call's or its own, so it passes through
// unchanged; anything else is a fault in the script, and Redis labels those.
func respScriptFailure(failure, timeout error) string {
	if timeout != nil {
		return fmt.Sprintf("ERR Script exceeded the %s time limit and was stopped", respScriptTimeout)
	}

	var apiErr *lua.ApiError
	if errors.As(failure, &apiErr) {
		if table, ok := apiErr.Object.(*lua.LTable); ok {
			if message, isString := table.RawGetString(respLuaErrField).(lua.LString); isString {
				return string(message)
			}
		}
	}

	return "ERR Error running script: " + failure.Error()
}

// respLuaStrings builds the KEYS or ARGV table, which Lua indexes from one.
func respLuaStrings(state *lua.LState, values [][]byte) *lua.LTable {
	table := state.NewTable()
	for _, value := range values {
		table.Append(lua.LString(value))
	}

	return table
}

// respLuaField builds the one field table Redis uses for a reply type Lua cannot express.
func respLuaField(state *lua.LState, field, value string) *lua.LTable {
	table := state.NewTable()
	table.RawSetString(field, lua.LString(value))

	return table
}

// respLuaReplyField builds redis.error_reply and redis.status_reply.
func respLuaReplyField(field string) lua.LGFunction {
	return func(state *lua.LState) int {
		state.Push(respLuaField(state, field, state.CheckString(1)))

		return 1
	}
}

func respLuaSHA1Hex(state *lua.LState) int {
	state.Push(lua.LString(respScriptHash(state.CheckString(1))))

	return 1
}

// respLuaJSONNull is what a JSON null decodes to, as in cjson: a value that is not nil, so a
// null inside an array does not end the array where it sits. One shared instance is enough,
// since it is only ever compared against itself and no script can reach inside it.
var respLuaJSONNull = &lua.LUserData{Value: "cjson.null"}

// respCJSONTable builds the cjson library. Scripts that keep a structured value under one key
// are the common use for EVAL, and encoding it by hand in Lua is what they would do instead.
func respCJSONTable(state *lua.LState) *lua.LTable {
	table := state.NewTable()
	state.SetFuncs(table, map[string]lua.LGFunction{
		"encode": respLuaJSONEncode,
		"decode": respLuaJSONDecode,
	})
	table.RawSetString("null", respLuaJSONNull)

	return table
}

func respLuaJSONDecode(state *lua.LState) int {
	var decoded any
	if err := json.Unmarshal([]byte(state.CheckString(1)), &decoded); err != nil {
		state.RaiseError("cjson: %s", err.Error())
	}
	state.Push(respJSONToLua(state, decoded))

	return 1
}

func respLuaJSONEncode(state *lua.LState) int {
	value, err := respLuaToJSON(state.CheckAny(1), 0)
	if err == nil {
		var encoded []byte
		// ponytail: encoding/json replaces a byte sequence that is not UTF-8 with U+FFFD, where
		// cjson passes it through. A script encoding a binary value gets the replacement; hand the
		// value to the client and encode it there if that matters.
		if encoded, err = json.Marshal(value); err == nil {
			state.Push(lua.LString(encoded))

			return 1
		}
	}
	state.RaiseError("cjson: %s", err.Error())

	return 0
}

// respJSONToLua converts what encoding/json decoded into Lua: an object into a table keyed by
// string, an array into one indexed from 1, the way cjson does.
func respJSONToLua(state *lua.LState, value any) lua.LValue {
	switch typed := value.(type) {
	case bool:
		return lua.LBool(typed)
	case float64:
		return lua.LNumber(typed)
	case string:
		return lua.LString(typed)
	case []any:
		table := state.NewTable()
		for _, item := range typed {
			table.Append(respJSONToLua(state, item))
		}

		return table
	case map[string]any:
		table := state.NewTable()
		for key, item := range typed {
			table.RawSetString(key, respJSONToLua(state, item))
		}

		return table
	default:
		// A JSON null, which is the only other thing encoding/json produces.
		return respLuaJSONNull
	}
}

// respLuaToJSON converts a Lua value into what encoding/json can marshal. The depth bound is
// what stops a table holding itself, which has no JSON spelling and would otherwise recurse
// until the stack gave out.
func respLuaToJSON(value lua.LValue, depth int) (any, error) {
	if depth > respLuaMaxDepth {
		return nil, errors.New("cannot serialize, excessive nesting")
	}

	switch typed := value.(type) {
	case *lua.LNilType:
		return nil, nil
	case lua.LBool:
		return bool(typed), nil
	case lua.LNumber:
		return float64(typed), nil
	case lua.LString:
		return string(typed), nil
	case *lua.LTable:
		return respLuaTableToJSON(typed, depth)
	case *lua.LUserData:
		if typed == respLuaJSONNull {
			return nil, nil
		}
	}

	return nil, fmt.Errorf("cannot serialize %s: type not supported", value.Type())
}

// respLuaTableToJSON decides whether a table is an array or an object. Lua spells both the same
// way, so the rule cjson uses stands here: a table whose keys are exactly 1..n is an array, and
// anything else, an empty table included, is an object.
func respLuaTableToJSON(table *lua.LTable, depth int) (any, error) {
	length, keys := table.Len(), 0
	table.ForEach(func(lua.LValue, lua.LValue) { keys++ })

	if length == keys && keys > 0 {
		items := make([]any, 0, length)
		for at := 1; at <= length; at++ {
			item, err := respLuaToJSON(table.RawGetInt(at), depth+1)
			if err != nil {
				return nil, err
			}
			items = append(items, item)
		}

		return items, nil
	}

	var err error

	object := make(map[string]any, keys)
	table.ForEach(func(key, item lua.LValue) {
		if err != nil {
			return
		}

		var converted any
		if converted, err = respLuaToJSON(item, depth+1); err == nil {
			object[key.String()] = converted
		}
	})

	return object, err
}

// respScriptHash is the digest a script is cached and looked up by.
func respScriptHash(body string) string {
	//nolint:gosec // The digest names a cache entry; SHA1 is what the EVALSHA protocol specifies.
	sum := sha1.Sum([]byte(body))

	return hex.EncodeToString(sum[:])
}

// respCompileScript reports whether body is valid Lua, without running any of it.
func respCompileScript(body string) error {
	state := lua.NewState(lua.Options{SkipOpenLibs: true})
	defer state.Close()

	_, err := state.LoadString(body)

	return err
}
