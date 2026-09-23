package discordbridge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf16"
)

func (s *Service) request(ctx context.Context, method, route string, body any, botAuth bool, result any) error {
	var encoded []byte
	var err error
	if body != nil {
		encoded, err = json.Marshal(body)
		if err != nil {
			return errors.New("Discord request could not be encoded")
		}
	}
	for attempt := 0; attempt < 2; attempt++ {
		s.rateMu.Lock()
		wait := time.Until(s.rateUntil)
		s.rateMu.Unlock()
		if wait > 0 {
			timer := time.NewTimer(wait)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}
		req, err := http.NewRequestWithContext(ctx, method, s.apiBase+route, bytes.NewReader(encoded))
		if err != nil {
			return errors.New("Discord request could not be created")
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "Hermetrix Discord Bridge")
		if botAuth {
			token, ok := s.vault.Get(tokenRef)
			if !ok {
				return errors.New("Discord bot token is missing")
			}
			req.Header.Set("Authorization", "Bot "+token)
		}
		response, err := s.client.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return errors.New("Discord connection failed; check network access")
		}
		data, readErr := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
		response.Body.Close()
		if readErr != nil {
			return errors.New("Discord response could not be read")
		}
		if len(data) > 1<<20 {
			return errors.New("Discord response exceeds the size limit")
		}
		if response.StatusCode == http.StatusTooManyRequests && attempt == 0 {
			var rate struct {
				RetryAfter float64 `json:"retry_after"`
				Global     bool    `json:"global"`
			}
			if json.Unmarshal(data, &rate) == nil && rate.RetryAfter > 0 {
				if rate.Global {
					delay := time.Duration(rate.RetryAfter * float64(time.Second))
					if delay > 2*time.Minute {
						delay = 2 * time.Minute
					}
					s.rateMu.Lock()
					s.rateUntil = time.Now().Add(delay)
					s.rateMu.Unlock()
				}
				if rate.RetryAfter <= 2 {
					timer := time.NewTimer(time.Duration(rate.RetryAfter * float64(time.Second)))
					select {
					case <-ctx.Done():
						timer.Stop()
						return ctx.Err()
					case <-timer.C:
						continue
					}
				}
			}
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			return fmt.Errorf("Discord request returned HTTP %d", response.StatusCode)
		}
		if result != nil && len(data) > 0 {
			if json.Unmarshal(data, result) != nil {
				return errors.New("Discord returned an invalid response")
			}
		}
		return nil
	}
	return errors.New("Discord rate limit prevented the request")
}
func commandDefinition() map[string]any {
	options := []map[string]any{}
	for _, item := range []struct{ name, description, arg string }{{"new", "Start a new session for the configured project", ""}, {"ask", "Ask the local project assistant", "prompt"}, {"status", "Show session status and pending approval", ""}, {"approve", "Approve the exact previously shown change", "approval_id"}, {"deny", "Deny the previously shown change", "approval_id"}, {"cancel", "Cancel this user's running bridge request", ""}} {
		option := map[string]any{"type": 1, "name": item.name, "description": item.description}
		if item.arg != "" {
			maximum := 200
			if item.arg == "prompt" {
				maximum = 4000
			}
			option["options"] = []map[string]any{{"type": 3, "name": item.arg, "description": item.arg, "required": true, "min_length": 1, "max_length": maximum}}
		}
		options = append(options, option)
	}
	return map[string]any{"name": "hermetrix", "type": 1, "description": "Control the configured local Hermetrix project", "options": options}
}
func (s *Service) RegisterCommands(ctx context.Context) error {
	cfg, owner, err := s.loadConfig(ctx)
	if err != nil {
		return err
	}
	if err := validateConfig(cfg, true); err != nil {
		return err
	}
	if err := s.backend.Validate(ctx, binding(cfg, owner)); err != nil {
		return err
	}
	return s.registerCommands(ctx, cfg)
}
func (s *Service) registerCommands(ctx context.Context, cfg Config) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	for _, guild := range cfg.GuildIDs {
		if err := s.request(ctx, http.MethodPost, "/applications/"+cfg.ApplicationID+"/guilds/"+guild+"/commands", commandDefinition(), true, nil); err != nil {
			return err
		}
	}
	return nil
}
func (s *Service) acknowledge(ctx context.Context, event interaction, content string, deferred bool) error {
	kind := 4
	data := map[string]any{"flags": 64, "allowed_mentions": map[string]any{"parse": []string{}}}
	if deferred {
		kind = 5
	} else {
		data["content"] = truncateMessage(content, 1900)
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return s.request(ctx, http.MethodPost, "/interactions/"+event.ID+"/"+url.PathEscape(event.Token)+"/callback", map[string]any{"type": kind, "data": data}, false, nil)
}
func (s *Service) editResponse(ctx context.Context, event interaction, content string) error {
	return s.request(ctx, http.MethodPatch, "/webhooks/"+event.ApplicationID+"/"+url.PathEscape(event.Token)+"/messages/@original", map[string]any{"content": truncateMessage(content, 1900), "allowed_mentions": map[string]any{"parse": []string{}}}, false, nil)
}
func textUnits(text string) int { return len(utf16.Encode([]rune(text))) }
func truncateMessage(text string, limit int) string {
	if textUnits(text) <= limit {
		return text
	}
	const suffix = "\n… Open Hermetrix locally for the complete response."
	budget := limit - textUnits(suffix)
	used, end := 0, 0
	for index, r := range text {
		units := 1
		if r > 0xffff {
			units = 2
		}
		if used+units > budget {
			end = index
			break
		}
		used += units
		end = index + len(string(r))
	}
	return text[:end] + suffix
}
func safeGatewayURL(raw string, test bool) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.User != nil {
		return "", errors.New("invalid Discord gateway URL")
	}
	if test && (u.Scheme == "ws" || u.Scheme == "wss") && (u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost") {
		query := u.Query()
		query.Set("v", "10")
		query.Set("encoding", "json")
		u.RawQuery = query.Encode()
		return u.String(), nil
	}
	if u.Scheme != "wss" || (u.Hostname() != "gateway.discord.gg" && !strings.HasSuffix(u.Hostname(), ".discord.gg")) || (u.Port() != "" && u.Port() != "443") {
		return "", errors.New("Discord gateway must use an official secure Discord host")
	}
	u.RawQuery = "v=10&encoding=json"
	u.Fragment = ""
	return u.String(), nil
}
