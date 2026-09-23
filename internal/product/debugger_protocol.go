package product

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// inspectorClient has one reader so asynchronous pause events cannot consume a
// command response (and command responses cannot discard pause events).
type inspectorClient struct {
	conn    *websocket.Conn
	mu      sync.Mutex
	writeMu sync.Mutex
	next    int
	pending map[int]chan inspectorMessage
	done    chan struct{}
	onEvent func(string, json.RawMessage)
}

type inspectorMessage struct {
	ID     int             `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func newInspectorClient(conn *websocket.Conn, onEvent func(string, json.RawMessage)) *inspectorClient {
	c := &inspectorClient{conn: conn, pending: map[int]chan inspectorMessage{}, done: make(chan struct{}), onEvent: onEvent}
	conn.SetReadLimit(4 << 20)
	go c.read()
	return c
}

func (c *inspectorClient) read() {
	defer close(c.done)
	defer c.conn.Close()
	for {
		var message inspectorMessage
		if err := c.conn.ReadJSON(&message); err != nil {
			return
		}
		if message.ID != 0 {
			c.mu.Lock()
			ch := c.pending[message.ID]
			c.mu.Unlock()
			if ch != nil {
				ch <- message
			}
		} else if c.onEvent != nil {
			c.onEvent(message.Method, message.Params)
		}
	}
}

func (c *inspectorClient) call(ctx context.Context, method string, params any, result any) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	c.mu.Lock()
	c.next++
	id := c.next
	ch := make(chan inspectorMessage, 1)
	c.pending[id] = ch
	c.mu.Unlock()
	defer func() { c.mu.Lock(); delete(c.pending, id); c.mu.Unlock() }()
	c.writeMu.Lock()
	_ = c.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	err := c.conn.WriteJSON(map[string]any{"id": id, "method": method, "params": params})
	c.writeMu.Unlock()
	if err != nil {
		return err
	}
	select {
	case message := <-ch:
		if message.Error != nil {
			return fmt.Errorf("debugger %s: %s", method, message.Error.Message)
		}
		if result != nil {
			return json.Unmarshal(message.Result, result)
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-c.done:
		return errors.New("debugger connection ended")
	}
}

func (c *inspectorClient) close() { _ = c.conn.Close() }
