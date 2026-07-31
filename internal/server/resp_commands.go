package server

import (
	"fmt"
	"maps"
	"runtime"
	"strconv"
	"strings"
	"sync"

	"github.com/skyoo2003/kvs"
	"github.com/skyoo2003/kvs/pkg/resp"
)

const (
	respCRLF           = "\r\n"
	respModeStandalone = "standalone"
	respRoleMaster     = "master"

	respErrNoProto = "NOPROTO unsupported protocol version"

	// respInfo names both the INFO command and the CLIENT INFO subcommand.
	respInfo = "INFO"

	// respDefaultUser is the only user kvs knows, matching the Redis built-in.
	respDefaultUser = "default"

	// respSubSetName is the CLIENT and HELLO clause that names a connection.
	respSubSetName = "SETNAME"

	// Command names a handler has to recognize, because one handler covers a family of
	// commands that differ only in unit or in whether an existing key blocks the write.
	respCmdDecr      = "DECR"
	respCmdDecrBy    = "DECRBY"
	respCmdExpire    = "EXPIRE"
	respCmdExpireAt  = "EXPIREAT"
	respCmdMSetNX    = "MSETNX"
	respCmdPersist   = "PERSIST"
	respCmdPExpire   = "PEXPIRE"
	respCmdPExpireAt = "PEXPIREAT"
	respCmdPSetEX    = "PSETEX"
	respCmdPTTL      = "PTTL"
	respCmdRenameNX  = "RENAMENX"
	respCmdSet       = "SET"
	respCmdType      = "TYPE"

	respCmdHKeys            = "HKEYS"
	respCmdHVals            = "HVALS"
	respCmdIncr             = "INCR"
	respCmdLPop             = "LPOP"
	respCmdSInter           = "SINTER"
	respCmdSUnion           = "SUNION"
	respCmdZRevRange        = "ZREVRANGE"
	respCmdZRevRangeByScore = "ZREVRANGEBYSCORE"
	respCmdZRevRank         = "ZREVRANK"
	respCmdZScore           = "ZSCORE"

	respCmdAuth         = "AUTH"
	respCmdDiscard      = "DISCARD"
	respCmdExec         = "EXEC"
	respCmdHello        = "HELLO"
	respCmdMulti        = "MULTI"
	respCmdPing         = "PING"
	respCmdPSubscribe   = "PSUBSCRIBE"
	respCmdPUnsubscribe = "PUNSUBSCRIBE"
	respCmdQuit         = "QUIT"
	respCmdReset        = "RESET"
	respCmdSubscribe    = "SUBSCRIBE"
	respCmdUnsubscribe  = "UNSUBSCRIBE"
	respCmdWatch        = "WATCH"
)

// respCommands is the dispatch table, built on first use by respCommandFor.
//
// It cannot be an initialized variable: EXEC runs its queued commands back through this
// same table, so the initializer would depend on itself and Go rejects that as an
// initialization cycle. An init function would sidestep it, but the linter forbids one, so
// the table is filled lazily instead.
var (
	respCommandsOnce sync.Once
	respCommands     map[string]respCommand
)

// respCommandFor looks up one command by its normalized name.
func respCommandFor(name string) (respCommand, bool) {
	respCommandsOnce.Do(func() {
		respCommands = make(map[string]respCommand)
		for _, group := range []map[string]respCommand{
			respConnectionCommands(), respTransactionTable(), respPubSubCommands(),
			respStringCommands(), respKeyspaceCommands(), respHashCommands(),
			respListCommands(), respSetCommands(), respZSetCommands(),
		} {
			maps.Copy(respCommands, group)
		}
	})

	cmd, ok := respCommands[name]

	return cmd, ok
}

// respConnectionCommands holds the connection and server commands.
func respConnectionCommands() map[string]respCommand {
	return map[string]respCommand{
		"CLIENT":     {run: (*respConn).cmdClient, minArgs: 2, maxArgs: -1},
		"COMMAND":    {run: (*respConn).cmdCommand, minArgs: 1, maxArgs: -1},
		"CONFIG":     {run: (*respConn).cmdConfig, minArgs: 2, maxArgs: -1},
		"ECHO":       {run: (*respConn).cmdEcho, minArgs: 2, maxArgs: 2},
		respCmdHello: {run: (*respConn).cmdHello, minArgs: 1, maxArgs: -1},
		respInfo:     {run: (*respConn).cmdInfo, minArgs: 1, maxArgs: -1},
		respCmdPing:  {run: (*respConn).cmdPing, minArgs: 1, maxArgs: 2},
		respCmdQuit:  {run: (*respConn).cmdQuit, minArgs: 1, maxArgs: 1},
		"SELECT":     {run: (*respConn).cmdSelect, minArgs: 2, maxArgs: 2},
		respCmdAuth:  {run: (*respConn).cmdAuth, minArgs: 2, maxArgs: 3},
		respCmdReset: {run: (*respConn).cmdReset, minArgs: 1, maxArgs: 1},
	}
}

// respTransactionTable holds the transactions commands.
func respTransactionTable() map[string]respCommand {
	return map[string]respCommand{
		respCmdMulti:   {run: (*respConn).cmdMulti, minArgs: 1, maxArgs: 1},
		respCmdExec:    {run: (*respConn).cmdExec, minArgs: 1, maxArgs: 1},
		respCmdDiscard: {run: (*respConn).cmdDiscard, minArgs: 1, maxArgs: 1},
		respCmdWatch:   {run: (*respConn).cmdWatch, minArgs: 2, maxArgs: -1},
		"UNWATCH":      {run: (*respConn).cmdUnwatch, minArgs: 1, maxArgs: 1},
	}
}

// respPubSubCommands holds the publish and subscribe commands.
func respPubSubCommands() map[string]respCommand {
	return map[string]respCommand{
		respCmdSubscribe:    {run: (*respConn).cmdSubscribe, minArgs: 2, maxArgs: -1},
		respCmdPSubscribe:   {run: (*respConn).cmdSubscribe, minArgs: 2, maxArgs: -1},
		respCmdUnsubscribe:  {run: (*respConn).cmdUnsubscribe, minArgs: 1, maxArgs: -1},
		respCmdPUnsubscribe: {run: (*respConn).cmdUnsubscribe, minArgs: 1, maxArgs: -1},
		"PUBLISH":           {run: (*respConn).cmdPublish, minArgs: 3, maxArgs: 3},
	}
}

// respStringCommands holds the strings commands.
func respStringCommands() map[string]respCommand {
	return map[string]respCommand{
		"APPEND":      {run: (*respConn).cmdAppend, minArgs: 3, maxArgs: 3},
		respCmdDecr:   {run: (*respConn).cmdIncr, minArgs: 2, maxArgs: 2},
		respCmdDecrBy: {run: (*respConn).cmdIncr, minArgs: 3, maxArgs: 3},
		respOptGet:    {run: (*respConn).cmdGet, minArgs: 2, maxArgs: 2},
		"GETDEL":      {run: (*respConn).cmdGetDel, minArgs: 2, maxArgs: 2},
		"GETEX":       {run: (*respConn).cmdGetEx, minArgs: 2, maxArgs: -1},
		"GETRANGE":    {run: (*respConn).cmdGetRange, minArgs: 4, maxArgs: 4},
		"GETSET":      {run: (*respConn).cmdGetSet, minArgs: 3, maxArgs: 3},
		respCmdIncr:   {run: (*respConn).cmdIncr, minArgs: 2, maxArgs: 2},
		"INCRBY":      {run: (*respConn).cmdIncr, minArgs: 3, maxArgs: 3},
		"INCRBYFLOAT": {run: (*respConn).cmdIncrByFloat, minArgs: 3, maxArgs: 3},
		"MGET":        {run: (*respConn).cmdMGet, minArgs: 2, maxArgs: -1},
		"MSET":        {run: (*respConn).cmdMSet, minArgs: 3, maxArgs: -1},
		respCmdMSetNX: {run: (*respConn).cmdMSet, minArgs: 3, maxArgs: -1},
		respCmdPSetEX: {run: (*respConn).cmdSetEX, minArgs: 4, maxArgs: 4},
		respCmdSet:    {run: (*respConn).cmdSet, minArgs: 3, maxArgs: -1},
		"SETEX":       {run: (*respConn).cmdSetEX, minArgs: 4, maxArgs: 4},
		"SETNX":       {run: (*respConn).cmdSetNX, minArgs: 3, maxArgs: 3},
		"SETRANGE":    {run: (*respConn).cmdSetRange, minArgs: 4, maxArgs: 4},
		"STRLEN":      {run: (*respConn).cmdStrLen, minArgs: 2, maxArgs: 2},
	}
}

// respKeyspaceCommands holds the keys and expiry commands.
func respKeyspaceCommands() map[string]respCommand {
	return map[string]respCommand{
		"COPY":           {run: (*respConn).cmdCopy, minArgs: 3, maxArgs: -1},
		"DBSIZE":         {run: (*respConn).cmdDBSize, minArgs: 1, maxArgs: 1},
		"DEL":            {run: (*respConn).cmdDel, minArgs: 2, maxArgs: -1},
		"EXISTS":         {run: (*respConn).cmdExists, minArgs: 2, maxArgs: -1},
		respCmdExpire:    {run: (*respConn).cmdExpire, minArgs: 3, maxArgs: 3},
		respCmdExpireAt:  {run: (*respConn).cmdExpire, minArgs: 3, maxArgs: 3},
		"FLUSHALL":       {run: (*respConn).cmdFlush, minArgs: 1, maxArgs: -1},
		"FLUSHDB":        {run: (*respConn).cmdFlush, minArgs: 1, maxArgs: -1},
		"KEYS":           {run: (*respConn).cmdKeys, minArgs: 2, maxArgs: 2},
		respCmdPersist:   {run: (*respConn).cmdPersist, minArgs: 2, maxArgs: 2},
		respCmdPExpire:   {run: (*respConn).cmdExpire, minArgs: 3, maxArgs: 3},
		respCmdPExpireAt: {run: (*respConn).cmdExpire, minArgs: 3, maxArgs: 3},
		respCmdPTTL:      {run: (*respConn).cmdTTL, minArgs: 2, maxArgs: 2},
		"RANDOMKEY":      {run: (*respConn).cmdRandomKey, minArgs: 1, maxArgs: 1},
		"RENAME":         {run: (*respConn).cmdRename, minArgs: 3, maxArgs: 3},
		respCmdRenameNX:  {run: (*respConn).cmdRename, minArgs: 3, maxArgs: 3},
		"SCAN":           {run: (*respConn).cmdScan, minArgs: 2, maxArgs: -1},
		"TTL":            {run: (*respConn).cmdTTL, minArgs: 2, maxArgs: 2},
		respCmdType:      {run: (*respConn).cmdType, minArgs: 2, maxArgs: 2},
		"UNLINK":         {run: (*respConn).cmdDel, minArgs: 2, maxArgs: -1},
	}
}

// respHashCommands holds the hashes commands.
func respHashCommands() map[string]respCommand {
	return map[string]respCommand{
		"HDEL":         {run: (*respConn).cmdHDel, minArgs: 3, maxArgs: -1},
		"HEXISTS":      {run: (*respConn).cmdHExists, minArgs: 3, maxArgs: 3},
		"HGET":         {run: (*respConn).cmdHGet, minArgs: 3, maxArgs: 3},
		"HGETALL":      {run: (*respConn).cmdHGetAll, minArgs: 2, maxArgs: 2},
		"HINCRBY":      {run: (*respConn).cmdHIncrBy, minArgs: 4, maxArgs: 4},
		"HINCRBYFLOAT": {run: (*respConn).cmdHIncrByFloat, minArgs: 4, maxArgs: 4},
		respCmdHKeys:   {run: (*respConn).cmdHGetAll, minArgs: 2, maxArgs: 2},
		"HLEN":         {run: (*respConn).cmdHLen, minArgs: 2, maxArgs: 2},
		"HMGET":        {run: (*respConn).cmdHMGet, minArgs: 3, maxArgs: -1},
		"HSCAN":        {run: (*respConn).cmdHScan, minArgs: 3, maxArgs: -1},
		"HSET":         {run: (*respConn).cmdHSet, minArgs: 4, maxArgs: -1},
		"HSETNX":       {run: (*respConn).cmdHSetNX, minArgs: 4, maxArgs: 4},
		respCmdHVals:   {run: (*respConn).cmdHGetAll, minArgs: 2, maxArgs: 2},
	}
}

// respListCommands holds the lists commands.
func respListCommands() map[string]respCommand {
	return map[string]respCommand{
		"LINDEX":    {run: (*respConn).cmdLIndex, minArgs: 3, maxArgs: 3},
		"LLEN":      {run: (*respConn).cmdLLen, minArgs: 2, maxArgs: 2},
		respCmdLPop: {run: (*respConn).cmdPop, minArgs: 2, maxArgs: 3},
		"LPUSH":     {run: (*respConn).cmdPush, minArgs: 3, maxArgs: -1},
		"LPUSHX":    {run: (*respConn).cmdPush, minArgs: 3, maxArgs: -1},
		"LRANGE":    {run: (*respConn).cmdLRange, minArgs: 4, maxArgs: 4},
		"LREM":      {run: (*respConn).cmdLRem, minArgs: 4, maxArgs: 4},
		"LSET":      {run: (*respConn).cmdLSet, minArgs: 4, maxArgs: 4},
		"LTRIM":     {run: (*respConn).cmdLTrim, minArgs: 4, maxArgs: 4},
		"RPOP":      {run: (*respConn).cmdPop, minArgs: 2, maxArgs: 3},
		"RPUSH":     {run: (*respConn).cmdPush, minArgs: 3, maxArgs: -1},
		"RPUSHX":    {run: (*respConn).cmdPush, minArgs: 3, maxArgs: -1},
	}
}

// respSetCommands holds the sets commands.
func respSetCommands() map[string]respCommand {
	return map[string]respCommand{
		"SADD":        {run: (*respConn).cmdSAdd, minArgs: 3, maxArgs: -1},
		"SCARD":       {run: (*respConn).cmdSCard, minArgs: 2, maxArgs: 2},
		"SDIFF":       {run: (*respConn).cmdSetOp, minArgs: 2, maxArgs: -1},
		respCmdSInter: {run: (*respConn).cmdSetOp, minArgs: 2, maxArgs: -1},
		"SISMEMBER":   {run: (*respConn).cmdSIsMember, minArgs: 3, maxArgs: 3},
		"SMEMBERS":    {run: (*respConn).cmdSMembers, minArgs: 2, maxArgs: 2},
		"SPOP":        {run: (*respConn).cmdSPop, minArgs: 2, maxArgs: 3},
		"SRANDMEMBER": {run: (*respConn).cmdSRandMember, minArgs: 2, maxArgs: 3},
		"SREM":        {run: (*respConn).cmdSRem, minArgs: 3, maxArgs: -1},
		"SSCAN":       {run: (*respConn).cmdSScan, minArgs: 3, maxArgs: -1},
		respCmdSUnion: {run: (*respConn).cmdSetOp, minArgs: 2, maxArgs: -1},
	}
}

// respZSetCommands holds the sorted sets commands.
func respZSetCommands() map[string]respCommand {
	return map[string]respCommand{
		"ZADD":                  {run: (*respConn).cmdZAdd, minArgs: 4, maxArgs: -1},
		"ZCARD":                 {run: (*respConn).cmdZCard, minArgs: 2, maxArgs: 2},
		"ZCOUNT":                {run: (*respConn).cmdZCount, minArgs: 4, maxArgs: 4},
		"ZINCRBY":               {run: (*respConn).cmdZIncrBy, minArgs: 4, maxArgs: 4},
		"ZMSCORE":               {run: (*respConn).cmdZScore, minArgs: 3, maxArgs: -1},
		"ZRANGE":                {run: (*respConn).cmdZRange, minArgs: 4, maxArgs: 5},
		"ZRANGEBYSCORE":         {run: (*respConn).cmdZRangeByScore, minArgs: 4, maxArgs: 5},
		"ZRANK":                 {run: (*respConn).cmdZRank, minArgs: 3, maxArgs: 3},
		"ZREM":                  {run: (*respConn).cmdZRem, minArgs: 3, maxArgs: -1},
		respCmdZRevRange:        {run: (*respConn).cmdZRange, minArgs: 4, maxArgs: 5},
		respCmdZRevRangeByScore: {run: (*respConn).cmdZRangeByScore, minArgs: 4, maxArgs: 5},
		respCmdZRevRank:         {run: (*respConn).cmdZRank, minArgs: 3, maxArgs: 3},
		"ZSCAN":                 {run: (*respConn).cmdZScan, minArgs: 3, maxArgs: -1},
		respCmdZScore:           {run: (*respConn).cmdZScore, minArgs: 3, maxArgs: 3},
	}
}

// respConfigParams are the settings CONFIG GET reports. kvs has no runtime configuration
// of its own; these exist because client tooling probes them on connect.
var respConfigParams = map[string]string{
	"appendonly":         "no",
	"databases":          "1",
	"maxclients":         strconv.Itoa(respMaxConns),
	"maxmemory":          "0",
	"maxmemory-policy":   "noeviction",
	"proto-max-bulk-len": strconv.Itoa(resp.MaxBulkLength),
	"save":               "",
	"timeout":            "0",
}

func (c *respConn) cmdPing(args [][]byte) error {
	if c.subscribed() {
		// A subscribed connection answers with a two element array, so the reply is shaped
		// like the pushed messages it gets interleaved with.
		payload := ""
		if len(args) == 2 {
			payload = string(args[1])
		}
		if err := c.writer.WriteArrayHeader(2); err != nil {
			return err
		}

		return c.writeBulkStrings("pong", payload)
	}

	if len(args) == 2 {
		return c.writer.WriteBulk(args[1])
	}

	return c.writer.WriteSimple(respPong)
}

func (c *respConn) cmdEcho(args [][]byte) error {
	return c.writer.WriteBulk(args[1])
}

func (c *respConn) cmdQuit(_ [][]byte) error {
	if err := c.writer.WriteSimple(respOK); err != nil {
		return err
	}

	return errRESPQuit
}

// cmdCommand answers the COMMAND introspection family with an empty reply. Clients send
// COMMAND DOCS while connecting and read an empty answer as "no hints available".
func (c *respConn) cmdCommand(_ [][]byte) error {
	return c.writer.WriteArrayHeader(0)
}

// cmdHello reports the server handshake. kvs speaks RESP2 only, so a client asking for
// RESP3 gets the NOPROTO reply that tells it to retry at a lower version.
func (c *respConn) cmdHello(args [][]byte) error {
	if len(args) > 1 {
		version, err := strconv.Atoi(string(args[1]))
		if err != nil || version != 2 {
			return c.writer.WriteError(respErrNoProto)
		}
	}
	if err := c.applyHelloOptions(args); err != nil {
		return c.writeFailure(err)
	}
	if !c.authed {
		return c.writer.WriteError(respErrNoAuth)
	}

	// RESP2 has no map type, so the handshake map is sent as a flattened array.
	if err := c.writer.WriteArrayHeader(14); err != nil {
		return err
	}

	fields := []string{
		"server", respServerName,
		"version", respRedisVersion,
		"proto", "2",
		"id", strconv.FormatInt(c.id, 10),
		"mode", respModeStandalone,
		"role", respRoleMaster,
	}
	for _, field := range fields {
		if err := c.writer.WriteBulkString(field); err != nil {
			return err
		}
	}
	if err := c.writer.WriteBulkString("modules"); err != nil {
		return err
	}

	return c.writer.WriteArrayHeader(0)
}

// applyHelloOptions handles the AUTH and SETNAME clauses HELLO accepts, which is how a
// client authenticates and names itself in the same round trip as the handshake.
// Nothing is applied until the whole option list has parsed, so a HELLO that fails on a later
// clause leaves neither the name nor the credential behind. Authenticating mid-parse meant a
// rejected handshake still answered on a connection it had quietly let in.
func (c *respConn) applyHelloOptions(args [][]byte) error {
	name := ""
	named := false
	var user, password []byte

	for i := 2; i < len(args); i++ {
		switch respUpper(args[i]) {
		case respCmdAuth:
			if i+2 >= len(args) {
				return errRESPSyntax
			}
			user, password = args[i+1], args[i+2]
			i += 2
		case respSubSetName:
			if i+1 >= len(args) {
				return errRESPSyntax
			}
			name, named = string(args[i+1]), true
			i++
		default:
			return errRESPSyntax
		}
	}

	if user != nil {
		if err := c.authenticate(user, password); err != nil {
			return err
		}
	}
	if named {
		c.name = name
	}

	return nil
}

func (c *respConn) cmdClient(args [][]byte) error {
	switch respUpper(args[1]) {
	case "ID":
		return c.writer.WriteInt(c.id)
	case "GETNAME":
		if c.name == "" {
			return c.writer.WriteNull()
		}

		return c.writer.WriteBulkString(c.name)
	case respSubSetName:
		if len(args) != 3 {
			return c.wrongArgs("client|setname")
		}
		c.name = string(args[2])

		return c.writer.WriteSimple(respOK)
	case "SETINFO":
		if len(args) != 4 {
			return c.wrongArgs("client|setinfo")
		}

		return c.writer.WriteSimple(respOK)
	case respInfo:
		return c.writer.WriteBulkString(fmt.Sprintf(
			"id=%d addr=%s laddr=%s name=%s db=0",
			c.id, c.netConn.RemoteAddr(), c.netConn.LocalAddr(), c.name,
		))
	default:
		return c.unknownSubcommand("client", args)
	}
}

// cmdSelect accepts only database 0. kvs holds a single keyspace, so there is no other
// database to switch to.
func (c *respConn) cmdSelect(args [][]byte) error {
	index, err := strconv.Atoi(string(args[1]))
	if err != nil {
		return c.writer.WriteError(respErrNotInteger)
	}
	if index != 0 {
		return c.writer.WriteError("ERR DB index is out of range")
	}

	return c.writer.WriteSimple(respOK)
}

// cmdInfo reports every section regardless of the requested one. Clients scan the reply
// for the fields they need, so a superset is safe.
func (c *respConn) cmdInfo(_ [][]byte) error {
	keyCount, expiring := 0, 0
	if err := c.read(func(tx *kvs.ReadTx) error {
		// Both counts come off the expiry index rather than a walk of the keyspace: only a key
		// carrying an expiry can be dead, so that index holds every candidate. Listing every key
		// here also allocated a slice of the whole keyspace, which made INFO a lever a client
		// could pull in a loop to hold the read lock.
		keyCount, expiring = tx.Len(), tx.Expiring()

		return nil
	}); err != nil {
		return c.writeFailure(err)
	}

	lines := []string{
		"# Server",
		"redis_version:" + respRedisVersion,
		"server_name:" + respServerName,
		"redis_mode:" + respModeStandalone,
		"os:" + runtime.GOOS,
		"arch_bits:" + strconv.Itoa(strconv.IntSize),
		"",
		"# Clients",
		"connected_clients:" + strconv.Itoa(c.server.connCount()),
		"",
		"# Replication",
		"role:" + respRoleMaster,
		"connected_slaves:0",
		"",
		"# Keyspace",
	}
	if keyCount > 0 {
		lines = append(lines, fmt.Sprintf("db0:keys=%d,expires=%d,avg_ttl=0", keyCount, expiring))
	}
	lines = append(lines, "")

	return c.writer.WriteBulkString(strings.Join(lines, respCRLF) + respCRLF)
}

func (c *respConn) cmdConfig(args [][]byte) error {
	switch respUpper(args[1]) {
	case "GET":
		if len(args) < 3 {
			return c.wrongArgs("config|get")
		}

		return c.writeConfigGet(args[2:])
	case respCmdSet:
		return c.writer.WriteError("ERR CONFIG SET is not supported")
	case "RESETSTAT":
		return c.writer.WriteSimple(respOK)
	default:
		return c.unknownSubcommand("config", args)
	}
}

// writeConfigGet matches parameter names exactly rather than by glob. Clients ask for
// specific settings, and an unknown name is simply left out of the reply, as Redis does.
func (c *respConn) writeConfigGet(params [][]byte) error {
	pairs := make([]string, 0, len(params)*2)
	for _, param := range params {
		name := strings.ToLower(string(param))
		if value, ok := respConfigParams[name]; ok {
			pairs = append(pairs, name, value)
		}
	}

	return c.writer.WriteStrings(pairs)
}
