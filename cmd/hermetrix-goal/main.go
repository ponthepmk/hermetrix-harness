// hermetrix-goal controls the server-owned durable coding loop. The client may
// disconnect without cancelling the goal; Stop is an explicit API operation.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"time"

	"hermetrix-harness/internal/taskcoord"
	"hermetrix-harness/internal/taskengine"
)

type client struct {
	base, token string
	http        *http.Client
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	if err := run(ctx, os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, out io.Writer) error {
	flags := flag.NewFlagSet("hermetrix-goal", flag.ContinueOnError)
	server := flags.String("server", "http://127.0.0.1:7331", "existing Hermetrix server; HTTPS required off loopback")
	spec := flags.String("spec", "", "JSON file containing task and runner; starts a new goal once")
	id := flags.String("goal", "", "existing goal ID")
	action := flags.String("action", "status", "status, watch, stop, resume (or use -spec to create)")
	revision := flags.Int("revision", 0, "exact current task revision for resume")
	if err := flags.Parse(args); err != nil {
		return err
	}
	u, err := url.Parse(*server)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || strings.Trim(u.Path, "/") != "" {
		return fmt.Errorf("server must be an HTTP(S) origin without credentials or path")
	}
	ip := net.ParseIP(u.Hostname())
	loopback := strings.EqualFold(u.Hostname(), "localhost") || ip != nil && ip.IsLoopback()
	if u.Scheme != "https" && !(u.Scheme == "http" && loopback) {
		return fmt.Errorf("remote goal control requires HTTPS")
	}
	c := client{base: strings.TrimRight(u.String(), "/"), token: os.Getenv("HERMETRIX_API_TOKEN"), http: &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
	if *spec != "" {
		if *id != "" || *action != "status" {
			return fmt.Errorf("-spec cannot be combined with existing-goal actions")
		}
		body, err := os.ReadFile(*spec)
		if err != nil {
			return err
		}
		if len(body) > 256<<10 {
			return fmt.Errorf("goal spec exceeds 256 KiB")
		}
		var input struct {
			Task   taskengine.CreateTaskInput `json:"task"`
			Runner taskcoord.StartGoalInput   `json:"runner"`
		}
		d := json.NewDecoder(bytes.NewReader(body))
		d.DisallowUnknownFields()
		if err = d.Decode(&input); err != nil {
			return err
		}
		if d.Decode(new(any)) != io.EOF {
			return fmt.Errorf("goal spec must contain one JSON object")
		}
		if input.Task.Actor == "" {
			input.Task.Actor = "goal-cli"
		}
		if input.Runner.Actor == "" {
			input.Runner.Actor = input.Task.Actor
		}
		if input.Runner.TaskID != "" {
			return fmt.Errorf("a new goal spec must not contain runner.task_id")
		}
		var task taskengine.Task
		if err = c.request(ctx, http.MethodPost, "/api/tasks", input.Task, &task); err != nil {
			return fmt.Errorf("create task (do not retry an uncertain request without inspecting the task list): %w", err)
		}
		fmt.Fprintf(out, "task_id=%s\n", task.ID)
		input.Runner.ExpectedTaskRevision = task.Revision
		var goal taskengine.GoalRun
		if err = c.request(ctx, http.MethodPost, "/api/tasks/"+url.PathEscape(task.ID)+"/goals", input.Runner, &goal); err != nil {
			return fmt.Errorf("task %s was created; inspect its goal status before resubmitting: %w", task.ID, err)
		}
		return json.NewEncoder(out).Encode(goal)
	}
	if *id == "" || len(*id) > 200 || strings.ContainsAny(*id, "/\\?#%") {
		return fmt.Errorf("a valid -goal ID is required")
	}
	path := "/api/goals/" + url.PathEscape(*id)
	switch *action {
	case "stop", "resume":
		if *action == "resume" && *revision < 1 {
			return fmt.Errorf("resume requires -revision from the current task")
		}
		var result taskengine.GoalRun
		if err = c.request(ctx, http.MethodPost, path+"/"+*action, map[string]int{"expected_task_revision": *revision}, &result); err != nil {
			return err
		}
		return json.NewEncoder(out).Encode(result)
	case "status", "watch":
		var last string
		for {
			var result struct {
				Goal         taskengine.GoalRun           `json:"goal"`
				Actions      []taskengine.GoalAction      `json:"actions"`
				Verification *taskengine.GoalVerification `json:"verification,omitempty"`
			}
			if err = c.request(ctx, http.MethodGet, path, nil, &result); err != nil {
				return err
			}
			encoded, _ := json.Marshal(result)
			if string(encoded) != last {
				if _, err = fmt.Fprintln(out, string(encoded)); err != nil {
					return err
				}
				last = string(encoded)
			}
			if *action == "status" {
				return nil
			}
			if result.Goal.State != taskengine.GoalActive && result.Goal.State != taskengine.GoalStopping {
				if result.Goal.State == taskengine.GoalCompleted {
					return nil
				}
				return fmt.Errorf("goal %s: %s (objective remains unconfirmed)", result.Goal.State, result.Goal.Reason)
			}
			timer := time.NewTimer(2 * time.Second)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}
	default:
		return fmt.Errorf("unknown action %q", *action)
	}
}

func (c client) request(ctx context.Context, method, path string, input, output any) error {
	var body io.Reader
	if input != nil {
		encoded, err := json.Marshal(input)
		if err != nil {
			return err
		}
		body = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, body)
	if err != nil {
		return err
	}
	if input != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	res, err := c.http.Do(req)
	if err != nil {
		return errors.New("goal API request failed; inspect the server before retrying")
	}
	defer res.Body.Close()
	encoded, err := io.ReadAll(io.LimitReader(res.Body, (2<<20)+1))
	if err != nil {
		return err
	}
	if len(encoded) > 2<<20 {
		return fmt.Errorf("goal API response exceeds 2 MiB")
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("goal API returned HTTP %d", res.StatusCode)
	}
	return json.Unmarshal(encoded, output)
}
