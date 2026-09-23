package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"golang.org/x/term"
	"hermetrix-harness/internal/agentplatform/piidentity"
	"hermetrix-harness/internal/mcp"
	"hermetrix-harness/internal/secrets"
	"hermetrix-harness/internal/store"
	_ "modernc.org/sqlite"
)

func main() {
	if len(os.Args) < 2 {
		fatal(errors.New("usage: hermetrix-h2 probe --read-only --data DIR --endpoint URL [--workspace DIR] | credential-store --data DIR"))
	}
	switch os.Args[1] {
	case "probe":
		fatal(probe(os.Args[2:]))
	case "credential-store":
		fatal(storeCredential(os.Args[2:]))
	default:
		fatal(errors.New("unknown H2A command"))
	}
}

func fatal(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func storeCredential(args []string) error {
	f := flag.NewFlagSet("credential-store", flag.ContinueOnError)
	data := f.String("data", ".hermetrix", "Hermetrix data directory")
	if err := f.Parse(args); err != nil {
		return err
	}
	if f.NArg() != 0 {
		return errors.New("unexpected credential-store argument")
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return errors.New("credential entry requires an interactive terminal")
	}
	fmt.Fprint(os.Stderr, "Enter Pi H2A read-only credential (hidden): ")
	secret, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return errors.New("could not read hidden credential")
	}
	defer func() {
		for i := range secret {
			secret[i] = 0
		}
	}()
	if len(secret) < 32 || strings.ContainsAny(string(secret), "\r\n \t") {
		return errors.New("credential format rejected")
	}
	// Create a dedicated local data root with the current schema before the
	// read-only probe. The credential itself is persisted only by the vault.
	dataStore, err := store.Open(context.Background(), *data)
	if err != nil {
		return errors.New("could not initialize local H2A data root")
	}
	if err := dataStore.Close(); err != nil {
		return errors.New("could not close local H2A data root")
	}
	vault, err := secrets.Open(*data)
	if err != nil {
		return errors.New("could not open Hermetrix vault")
	}
	if err := vault.Set(mcp.CredentialRef(piidentity.ServerID), string(secret)); err != nil {
		return errors.New("could not store credential in Hermetrix vault")
	}
	fmt.Fprintln(os.Stdout, "H2A credential stored in Hermetrix vault; value was not displayed")
	return nil
}

func probe(args []string) error {
	f := flag.NewFlagSet("probe", flag.ContinueOnError)
	readOnly := f.Bool("read-only", false, "required H2A safety mode")
	data := f.String("data", ".hermetrix", "existing Hermetrix data directory")
	endpoint := f.String("endpoint", "", "HTTPS or authenticated localhost tunnel /mcp URL")
	workspace := f.String("workspace", ".", "workspace to verify unchanged")
	if err := f.Parse(args); err != nil {
		return err
	}
	if !*readOnly || f.NArg() != 0 {
		return errors.New("H2A probe requires --read-only and no positional arguments")
	}
	if *endpoint == "" {
		return errors.New("H2A secure endpoint is required")
	}
	beforeWorkspace, err := workspaceDigest(*workspace)
	if err != nil {
		return err
	}
	beforeDB, err := dbCounts(*data)
	if err != nil {
		return err
	}
	caller, err := piidentity.NewMCPCaller(*data, *endpoint)
	if err != nil {
		return err
	}
	defer caller.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	first, err := piidentity.Probe(ctx, caller)
	if err != nil {
		return err
	}
	// A second full probe forces a fresh whoami. No previous identity is treated
	// as authority for the second connection or used to skip authorization.
	caller.Close()
	secondCaller, err := piidentity.NewMCPCaller(*data, *endpoint)
	if err != nil {
		return err
	}
	defer secondCaller.Close()
	second, err := piidentity.ProbeFresh(ctx, secondCaller)
	if err != nil {
		return err
	}
	second.FreshWhoamiCount = first.FreshWhoamiCount + second.FreshWhoamiCount
	second.RetryCount += first.RetryCount
	if strings.HasPrefix(*endpoint, "https://") {
		second.TransportMode = "https"
		second.EndpointClass = "HTTPS endpoint"
	} else {
		second.TransportMode = "loopback HTTP"
		second.EndpointClass = "loopback"
	}
	afterDB, err := dbCounts(*data)
	if err != nil {
		return err
	}
	afterWorkspace, err := workspaceDigest(*workspace)
	if err != nil {
		return err
	}
	second.TaskRunsDelta = afterDB["task_runs"] - beforeDB["task_runs"]
	second.AttemptsDelta = afterDB["task_step_attempts"] - beforeDB["task_step_attempts"]
	second.EffectsDelta = afterDB["task_effect_intents"] - beforeDB["task_effect_intents"]
	second.RunUpdatesSent = afterDB["agent_platform_outbox"] - beforeDB["agent_platform_outbox"]
	second.WorkspaceIdentical = beforeWorkspace == afterWorkspace
	if second.TaskRunsDelta != 0 || second.AttemptsDelta != 0 || second.EffectsDelta != 0 || second.RunUpdatesSent != 0 || !second.WorkspaceIdentical {
		return errors.New("H2A before/after safety invariant failed")
	}
	encoded, err := json.Marshal(second)
	if err != nil {
		return errors.New("cannot encode H2A receipt")
	}
	if err := secondCaller.ScanForLeak(*workspace, *data, encoded); err != nil {
		return err
	}
	_, err = os.Stdout.Write(append(encoded, '\n'))
	return err
}

func dbCounts(data string) (map[string]int, error) {
	path, err := filepath.Abs(filepath.Join(data, "hermetrix.db"))
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(path); err != nil {
		return nil, errors.New("existing Hermetrix database is required for H2A safety snapshot")
	}
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?mode=ro")
	if err != nil {
		return nil, errors.New("cannot open Hermetrix database read-only")
	}
	defer db.Close()
	counts := map[string]int{}
	for _, table := range []string{"task_runs", "task_step_attempts", "task_effect_intents", "agent_platform_outbox"} {
		var count int
		var exists int
		if err := db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&exists); err != nil {
			return nil, errors.New("cannot read H2A safety counter")
		}
		if exists == 0 {
			if table != "agent_platform_outbox" {
				return nil, errors.New("required Hermetrix safety counter is absent")
			}
			counts[table] = 0
			continue
		}
		if err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil {
			return nil, errors.New("cannot read H2A safety counter")
		}
		counts[table] = count
	}
	return counts, nil
}

func workspaceDigest(root string) (string, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	var entries []string
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", ".hermetrix", ".cache", "node_modules":
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			entries = append(entries, filepath.ToSlash(rel)+"\x00link\x00"+target)
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(body)
		entries = append(entries, filepath.ToSlash(rel)+"\x00"+hex.EncodeToString(sum[:]))
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(entries)
	sum := sha256.Sum256([]byte(strings.Join(entries, "\n")))
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}
