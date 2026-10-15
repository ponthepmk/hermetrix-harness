package product

import (
	"context"
	"fmt"
	"time"

	"hermetrix-harness/internal/agentruntime"
)

func dispatchBrowserKey(ctx context.Context, client *cdpClient, input BrowserActionInput) error {
	key, code, virtual, ok := agentruntime.BrowserKey(input.Text)
	if !ok {
		return fmt.Errorf("unsupported browser key %q", input.Text)
	}
	args := map[string]any{"key": key, "code": code, "windowsVirtualKeyCode": virtual, "nativeVirtualKeyCode": virtual, "type": "rawKeyDown"}
	if input.Text == "Enter" {
		args["type"] = "keyDown"
		args["text"] = "\r"
		args["unmodifiedText"] = "\r"
	}
	releaseAttempted := false
	// Neutralize the held input on cancellation too. Never replay keyDown.
	defer func() {
		if !releaseAttempted {
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
			defer cancel()
			up := map[string]any{"type": "keyUp", "key": key, "code": code, "windowsVirtualKeyCode": virtual, "nativeVirtualKeyCode": virtual}
			_ = client.call(cleanup, "Input.dispatchKeyEvent", up, nil)
		}
	}()
	if err := client.call(ctx, "Input.dispatchKeyEvent", args, nil); err != nil {
		return err
	}
	if input.DurationMS > 0 {
		timer := time.NewTimer(time.Duration(input.DurationMS) * time.Millisecond)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
		}
	}
	args["type"] = "keyUp"
	delete(args, "text")
	delete(args, "unmodifiedText")
	releaseAttempted = true
	return client.call(ctx, "Input.dispatchKeyEvent", args, nil)
}
