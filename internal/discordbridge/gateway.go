package discordbridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type gatewayPacket struct {
	Op       int             `json:"op"`
	Data     json.RawMessage `json:"d"`
	Sequence *int64          `json:"s"`
	Type     string          `json:"t"`
}
type gatewayResume struct {
	sessionID, url string
	sequence       *int64
}
type gatewayFailure struct {
	message      string
	fatal, reset bool
}

func (e *gatewayFailure) Error() string { return e.message }

func (s *Service) runGateway(ctx context.Context, cfg Config, owner string) {
	if err := s.registerCommands(ctx, cfg); err != nil {
		if ctx.Err() == nil {
			s.setState("error", err.Error())
		}
		return
	}
	var gateway struct {
		URL               string `json:"url"`
		SessionStartLimit struct {
			Remaining int `json:"remaining"`
		} `json:"session_start_limit"`
	}
	if err := s.request(ctx, http.MethodGet, "/gateway/bot", nil, true, &gateway); err != nil {
		if ctx.Err() == nil {
			s.setState("error", err.Error())
		}
		return
	}
	endpoint, err := safeGatewayURL(gateway.URL, s.allowTestEndpoints)
	if err != nil {
		s.setState("error", err.Error())
		return
	}
	if gateway.SessionStartLimit.Remaining <= 0 {
		s.setState("error", "Discord session start limit is exhausted; reconnect after it resets.")
		return
	}
	resume := gatewayResume{}
	delay := s.reconnectDelay
	fresh := 0
	for ctx.Err() == nil {
		if resume.sessionID == "" {
			fresh++
			if fresh > 5 {
				s.setState("error", "Discord repeatedly rejected a fresh connection; check configuration before reconnecting.")
				return
			}
		}
		url := endpoint
		if resume.url != "" {
			url = resume.url
		}
		err := s.gatewayConnection(ctx, cfg, owner, url, &resume)
		if ctx.Err() != nil {
			return
		}
		var failure *gatewayFailure
		if errors.As(err, &failure) {
			if failure.fatal {
				s.setState("error", failure.message)
				return
			}
			if failure.reset {
				resume = gatewayResume{}
				if delay < 5*time.Second {
					delay = 5 * time.Second
				}
			}
		}
		s.setState("reconnecting", "Discord connection interrupted; reconnecting with replay protection.")
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		delay *= 2
		if delay > 60*time.Second {
			delay = 60 * time.Second
		}
	}
}

func (s *Service) gatewayConnection(ctx context.Context, cfg Config, owner, endpoint string, resume *gatewayResume) error {
	conn, _, err := s.dialer.DialContext(ctx, endpoint, nil)
	if err != nil {
		return errors.New("Discord gateway could not connect")
	}
	defer conn.Close()
	connectionCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() { <-connectionCtx.Done(); _ = conn.Close() }()
	conn.SetReadLimit(1 << 20)
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	var hello gatewayPacket
	if err := conn.ReadJSON(&hello); err != nil || hello.Op != 10 {
		return errors.New("Discord gateway did not send Hello")
	}
	var timing struct {
		HeartbeatInterval int `json:"heartbeat_interval"`
	}
	if json.Unmarshal(hello.Data, &timing) != nil || timing.HeartbeatInterval < 10 || timing.HeartbeatInterval > 300000 {
		return &gatewayFailure{message: "Discord supplied an invalid heartbeat interval", fatal: true}
	}
	_ = conn.SetReadDeadline(time.Time{})
	var writeMu, stateMu sync.Mutex
	acknowledged := true
	seq := resume.sequence
	send := func(op int, data any) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		return conn.WriteJSON(map[string]any{"op": op, "d": data})
	}
	heartbeat := func(checkACK bool) error {
		stateMu.Lock()
		if checkACK && !acknowledged {
			stateMu.Unlock()
			return errors.New("Discord heartbeat was not acknowledged")
		}
		acknowledged = false
		current := seq
		stateMu.Unlock()
		return send(1, current)
	}
	interval := time.Duration(timing.HeartbeatInterval) * time.Millisecond
	go func() {
		timer := time.NewTimer(time.Duration(rand.Float64() * float64(interval)))
		defer timer.Stop()
		for {
			select {
			case <-connectionCtx.Done():
				return
			case <-timer.C:
				if heartbeat(true) != nil {
					_ = conn.Close()
					return
				}
				timer.Reset(interval)
			}
		}
	}()
	token, ok := s.vault.Get(tokenRef)
	if !ok {
		return &gatewayFailure{message: "Discord bot token is missing", fatal: true}
	}
	if resume.sessionID != "" {
		err = send(6, map[string]any{"token": token, "session_id": resume.sessionID, "seq": resume.sequence})
	} else {
		err = send(2, map[string]any{"token": token, "intents": 0, "properties": map[string]string{"os": "hermetrix", "browser": "hermetrix", "device": "hermetrix"}})
	}
	if err != nil {
		return errors.New("Discord gateway handshake failed")
	}
	ready := resume.sessionID != ""
	for {
		var packet gatewayPacket
		if err := conn.ReadJSON(&packet); err != nil {
			var closed *websocket.CloseError
			if errors.As(err, &closed) {
				switch closed.Code {
				case 4004, 4010, 4011, 4012, 4013, 4014:
					return &gatewayFailure{message: fmt.Sprintf("Discord rejected the connection (code %d); check bot token, application and server setup.", closed.Code), fatal: true}
				case 4007, 4009:
					return &gatewayFailure{message: "Discord session expired", reset: true}
				}
			}
			return errors.New("Discord gateway disconnected")
		}
		if packet.Sequence != nil {
			copy := *packet.Sequence
			stateMu.Lock()
			seq = &copy
			stateMu.Unlock()
			resume.sequence = &copy
		}
		switch packet.Op {
		case 11:
			stateMu.Lock()
			acknowledged = true
			stateMu.Unlock()
		case 1:
			if heartbeat(false) != nil {
				return errors.New("Discord heartbeat could not be sent")
			}
		case 7:
			return errors.New("Discord requested reconnect")
		case 9:
			var resumable bool
			_ = json.Unmarshal(packet.Data, &resumable)
			return &gatewayFailure{message: "Discord invalidated the session", reset: !resumable}
		case 0:
			switch packet.Type {
			case "READY":
				var data struct {
					SessionID   string `json:"session_id"`
					ResumeURL   string `json:"resume_gateway_url"`
					Application struct {
						ID string `json:"id"`
					} `json:"application"`
					User struct {
						ID string `json:"id"`
					} `json:"user"`
				}
				if json.Unmarshal(packet.Data, &data) != nil || data.Application.ID != cfg.ApplicationID || data.SessionID == "" {
					return &gatewayFailure{message: "Discord bot does not match the configured application ID", fatal: true}
				}
				safe, err := safeGatewayURL(data.ResumeURL, s.allowTestEndpoints)
				if err != nil {
					return &gatewayFailure{message: err.Error(), fatal: true}
				}
				resume.sessionID = data.SessionID
				resume.url = safe
				ready = true
				s.mu.Lock()
				s.botID = data.User.ID
				s.state = "ready"
				s.lastError = ""
				s.mu.Unlock()
			case "RESUMED":
				ready = true
				s.setState("ready", "")
			case "INTERACTION_CREATE":
				if ready {
					var event interaction
					if json.Unmarshal(packet.Data, &event) == nil {
						s.handleInteraction(ctx, cfg, owner, event)
					}
				}
			}
		}
	}
}
