// hermetrix-worker reads a curated JSON task on stdin and emits an unverified
// proposal on stdout. It never modifies the source workspace.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"time"

	"hermetrix-harness/internal/providers"
	"hermetrix-harness/internal/secrets"
	"hermetrix-harness/internal/worker"
)

func main() {
	if err := run(); err != nil {
		var workerErr *worker.Error
		if errors.As(err, &workerErr) {
			_ = json.NewEncoder(os.Stderr).Encode(struct {
				Type string `json:"type"`
				*worker.Error
			}{Type: "error", Error: workerErr})
		} else {
			fmt.Fprintln(os.Stderr, err)
		}
		os.Exit(1)
	}
}
func run() error {
	base := flag.String("base-url", "", "OpenAI-compatible URL including /v1 when needed")
	model := flag.String("model", "", "model ID")
	env := flag.String("key-env", "HERMETRIX_WORKER_API_KEY", "credential environment variable name")
	data := flag.String("data", "", "optional existing Hermetrix vault directory (read only)")
	ref := flag.String("credential-ref", "", "explicit vault reference; overrides environment")
	timeout := flag.Duration("timeout", 4*time.Minute, "provider deadline (30s–15m)")
	emitProgress := flag.Bool("progress", true, "emit structured progress events on stderr")
	flag.Parse()
	u, err := url.Parse(*base)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || *model == "" {
		return fmt.Errorf("HTTPS base URL without credentials/query/fragment and model required")
	}
	key := os.Getenv(*env)
	if *ref != "" {
		if *data == "" {
			return fmt.Errorf("credential reference requires data directory")
		}
		vault, err := secrets.Open(*data)
		if err != nil {
			return fmt.Errorf("cannot read credential vault")
		}
		key, _ = vault.Get(*ref)
	}
	if key == "" {
		return fmt.Errorf("credential unavailable")
	}
	if *timeout < 30*time.Second || *timeout > 15*time.Minute {
		return fmt.Errorf("timeout must be between 30s and 15m")
	}
	raw, err := io.ReadAll(io.LimitReader(os.Stdin, 128*1024+1))
	if err != nil || len(raw) > 128*1024 {
		return fmt.Errorf("cannot read task or task too large")
	}
	var task worker.Task
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&task); err != nil || decoder.Decode(new(any)) != io.EOF {
		return fmt.Errorf("invalid task JSON")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()
	client := &http.Client{Timeout: *timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return fmt.Errorf("redirect denied") }}
	progressEncoder := json.NewEncoder(os.Stderr)
	result, err := worker.RunWithOptions(ctx, providers.NewOpenAIAdapter(client), providers.Profile{BaseURL: *base, Model: *model}, key, task, worker.Options{Progress: func(event worker.ProgressEvent) {
		if *emitProgress {
			_ = progressEncoder.Encode(struct {
				Type string `json:"type"`
				worker.ProgressEvent
			}{Type: "progress", ProgressEvent: event})
		}
	}, HeartbeatInterval: 15 * time.Second})
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(result)
}
