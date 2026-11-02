package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"time"

	"hermetrix-harness/internal/releaseinfo"
)

func runReleaseCommand(command string, args []string, output io.Writer) error {
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	data := flags.String("data", "", "data root to inspect without creating or migrating it")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || command == "version" && *data != "" {
		return fmt.Errorf("unexpected arguments")
	}
	manifest, err := releaseinfo.Current()
	if err != nil {
		return err
	}
	result := map[string]any{"release": manifest}
	if command == "preflight" {
		if *data == "" {
			return fmt.Errorf("preflight requires --data")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		inspection, err := releaseinfo.Inspect(ctx, *data)
		if err != nil {
			return err
		}
		result["data"] = inspection
	}
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	return encoder.Encode(result)
}
