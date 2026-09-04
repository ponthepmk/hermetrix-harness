package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"time"
)

// A stdio MCP server used to be launched once per call. That was correct and
// slow: `npx` resolving a package takes seconds, and paying it on every
// tools/call made a local server feel broken. It also made two MCP features
// impossible to build on top, because both need the connection to outlive one
// request: a server cannot ask the client a question, or ask it to sample a
// model, if the process it would ask through has already exited.
//
// The pool keeps one live process per server and hands it out under a lock, so
// a server that is stateless between calls stays stateless and one that is not
// keeps the state it is entitled to. A broken process is discarded. Discovery
// requests may reconnect immediately because they are read-only by contract;
// an effectful tools/call fails uncertain and only a later call starts fresh.

const (
	// poolIdleTimeout is how long an unused server process is kept alive. Long
	// enough that a conversation does not pay the launch twice, short enough
	// that a machine is not holding processes for a session finished hours ago.
	poolIdleTimeout = 5 * time.Minute
	poolSweepEvery  = 30 * time.Second
)

// pooledSession is one live server process plus the lock serialising access to
// it. A JSON-RPC stream over one pipe pair cannot be shared concurrently: two
// callers writing at once would interleave their frames.
type pooledSession struct {
	mu       sync.Mutex
	session  *stdioSession
	identity string
	lastUsed time.Time
}

type sessionPool struct {
	mu      sync.Mutex
	live    map[string]*pooledSession
	sweeper sync.Once
	stopped bool
}

func newSessionPool() *sessionPool { return &sessionPool{live: map[string]*pooledSession{}} }

// serverIdentity fingerprints the configuration a live process was started
// with. A user who edits the command, the token or the timeout gets a new
// process rather than the old one answering under the new settings.
func serverIdentity(server Server, credential string) string {
	return strings.Join([]string{server.TransportKind, server.Endpoint, server.APIKeyEnv,
		credential, server.ProtocolMode}, "\x00")
}

// acquire returns a live session for one server, launching it if there is none
// or if the configuration changed. The caller must call release.
func (c *Client) acquire(ctx context.Context, server Server, credential string) (*pooledSession, error) {
	identity := serverIdentity(server, credential)
	c.pool.mu.Lock()
	if c.pool.stopped {
		c.pool.mu.Unlock()
		return nil, errors.New("MCP client is shut down")
	}
	entry, ok := c.pool.live[server.ID]
	if ok && entry.identity != identity {
		delete(c.pool.live, server.ID)
		go entry.close()
		ok = false
	}
	if !ok {
		entry = &pooledSession{identity: identity}
		c.pool.live[server.ID] = entry
	}
	c.pool.startSweeper(c)
	c.pool.mu.Unlock()

	entry.mu.Lock()
	if entry.session == nil {
		session, err := c.startStdio(ctx, server, credential)
		if err != nil {
			entry.mu.Unlock()
			c.forget(server.ID)
			return nil, err
		}
		entry.session = session
	}
	entry.lastUsed = time.Now()
	return entry, nil
}

func (entry *pooledSession) release() { entry.lastUsed = time.Now(); entry.mu.Unlock() }

// discard stops the process behind a held entry, so the next acquire launches a
// fresh one. Called when a call fails in a way that says the pipe is gone.
func (entry *pooledSession) discard() {
	if entry.session != nil {
		entry.session.stop()
		entry.session = nil
	}
}

func (entry *pooledSession) close() {
	entry.mu.Lock()
	defer entry.mu.Unlock()
	entry.discard()
}

func (c *Client) forget(serverID string) {
	c.pool.mu.Lock()
	entry, ok := c.pool.live[serverID]
	delete(c.pool.live, serverID)
	c.pool.mu.Unlock()
	if ok {
		entry.close()
	}
}

// CloseServer drops any live process for one server. Saving new settings or
// deleting a connection must not leave the old process running.
func (c *Client) CloseServer(serverID string) { c.forget(serverID) }

// Close stops every pooled process. A server left running after Hermetrix exits
// is a process nobody owns.
func (c *Client) Close() {
	c.pool.mu.Lock()
	c.pool.stopped = true
	entries := make([]*pooledSession, 0, len(c.pool.live))
	for id, entry := range c.pool.live {
		entries = append(entries, entry)
		delete(c.pool.live, id)
	}
	c.pool.mu.Unlock()
	for _, entry := range entries {
		entry.close()
	}
}

func (p *sessionPool) startSweeper(client *Client) {
	p.sweeper.Do(func() {
		go func() {
			ticker := time.NewTicker(poolSweepEvery)
			defer ticker.Stop()
			for range ticker.C {
				if !client.sweepIdle() {
					return
				}
			}
		}()
	})
}

// sweepIdle closes processes nobody has used recently. It returns false once
// the pool is shut down, which stops the sweeper.
func (c *Client) sweepIdle() bool {
	c.pool.mu.Lock()
	if c.pool.stopped {
		c.pool.mu.Unlock()
		return false
	}
	cutoff := time.Now().Add(-poolIdleTimeout)
	stale := []*pooledSession{}
	for id, entry := range c.pool.live {
		if entry.mu.TryLock() {
			idle := entry.session != nil && entry.lastUsed.Before(cutoff)
			empty := entry.session == nil
			entry.mu.Unlock()
			if idle || empty {
				stale = append(stale, entry)
				delete(c.pool.live, id)
			}
		}
	}
	c.pool.mu.Unlock()
	for _, entry := range stale {
		entry.close()
	}
	return true
}

// pooledCall runs one RPC on the server's live process. It reconnects once only
// for catalog listing, whose MCP contract is read-only. In particular,
// tools/call is never replayed: a server may have completed its effect before
// dying on the response path, so retrying would turn an uncertain result into a
// duplicate effect.
func (c *Client) pooledCall(ctx context.Context, server Server, credential, method string,
	params map[string]any) (json.RawMessage, error) {
	for attempt := 0; attempt < 2; attempt++ {
		entry, err := c.acquire(ctx, server, credential)
		if err != nil {
			return nil, err
		}
		response, callErr := entry.session.call(ctx, rpcRequest{JSONRPC: "2.0", ID: c.nextID.Add(1),
			Method: method, Params: params})
		if callErr == nil {
			result := response.Result
			entry.release()
			return result, nil
		}
		broken := isBrokenConnection(callErr)
		cancelled := ctx.Err() != nil
		if broken || cancelled {
			entry.discard()
		}
		entry.release()
		if !broken || !retryableMCPMethod(method) || attempt == 1 || cancelled {
			return nil, callErr
		}
	}
	return nil, errors.New("unreachable")
}

// retryableMCPMethod is deliberately an allowlist. Catalog listing is the only
// family whose semantics make an automatic replay safe. Resource reads and
// prompt rendering are also observational in most servers, but the protocol
// permits server-side work and nested requests while producing them; reconnect
// on the caller's next attempt is safer than guessing that work was idempotent.
func retryableMCPMethod(method string) bool {
	switch method {
	case "tools/list", "resources/list", "prompts/list":
		return true
	default:
		return false
	}
}

// isBrokenConnection separates "this server is gone" from "this server said
// no". Only the first is worth reconnecting for: retrying a refusal would run
// the same effect twice.
func isBrokenConnection(err error) bool {
	if err == nil {
		return false
	}
	var wire *wireError
	if errors.As(err, &wire) {
		// A JSON-RPC error is the server answering, not the pipe failing.
		return false
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.ErrClosedPipe) {
		return true
	}
	text := strings.ToLower(err.Error())
	for _, marker := range []string{"broken pipe", "file already closed", "exited without answering",
		"connection reset", "eof", "process already finished"} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

// liveCount reports how many server processes the pool is holding. It exists
// for the tests: a pool that silently leaks processes is exactly the failure
// this file was written to prevent.
func (c *Client) liveCount() int {
	c.pool.mu.Lock()
	defer c.pool.mu.Unlock()
	count := 0
	for _, entry := range c.pool.live {
		if entry.mu.TryLock() {
			if entry.session != nil {
				count++
			}
			entry.mu.Unlock()
		} else {
			count++
		}
	}
	return count
}
