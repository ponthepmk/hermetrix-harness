package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"hermetrix-harness/internal/agent"
	"hermetrix-harness/internal/capabilities"
	ctxcompiler "hermetrix-harness/internal/context"
	"hermetrix-harness/internal/curator"
	"hermetrix-harness/internal/embedding"
	"hermetrix-harness/internal/fidelity"
	"hermetrix-harness/internal/learning"
	"hermetrix-harness/internal/localmodel"
	"hermetrix-harness/internal/mcp"
	"hermetrix-harness/internal/product"
	"hermetrix-harness/internal/providers"
	"hermetrix-harness/internal/qualification"
	"hermetrix-harness/internal/runtime"
	"hermetrix-harness/internal/secrets"
	"hermetrix-harness/internal/skills"
	"hermetrix-harness/internal/store"
	toolruntime "hermetrix-harness/internal/tools"
	"hermetrix-harness/internal/web"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	switch os.Args[1] {
	case "serve":
		runServe(os.Args[2:])
	case "taskeval":
		runTaskEval(os.Args[2:])
	case "corpus":
		if err := runCorpus(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "corpus:", err)
			os.Exit(1)
		}
	case "hostile":
		if err := runHostile(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "hostile:", err)
			os.Exit(1)
		}
	default:
		usage()
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage:")
	fmt.Fprintln(os.Stderr, "  hermetrix serve  [--data PATH] [--listen HOST:PORT] [--open] [--desktop]")
	fmt.Fprintln(os.Stderr, "  hermetrix corpus export --data PATH --out DIR")
	fmt.Fprintln(os.Stderr, "  hermetrix corpus score  --data PATH --dir DIR [--provider NAME]")
	fmt.Fprintln(os.Stderr, "  hermetrix taskeval generate --dir DIR [--per-class N] [--seed N]")
	fmt.Fprintln(os.Stderr, "  hermetrix taskeval score  --data PATH --dir DIR [--provider NAME] [--profile NAME]")
	fmt.Fprintln(os.Stderr, "  hermetrix hostile --data PATH [--provider NAME] [--workspace PATH]")
	os.Exit(2)
}

func runServe(args []string) {
	flags := flag.NewFlagSet("serve", flag.ExitOnError)
	defaultData, _ := filepath.Abs(".hermetrix")
	dataRoot := flags.String("data", defaultData, "local data directory")
	listen := flags.String("listen", "127.0.0.1:7331", "HTTP listen address")
	debug := flags.Bool("debug", false, "enable debug logging")
	workspace := flags.String("workspace", ".", "workspace root exposed to bounded core tools")
	providerName := flags.String("provider-name", "", "optional startup provider profile name")
	providerBaseURL := flags.String("provider-base-url", "", "OpenAI-compatible base URL, for example https://host/v1")
	providerModel := flags.String("provider-model", "", "model ID for the startup provider")
	providerAPIKeyEnv := flags.String("provider-api-key-env", "HERMETRIX_PROVIDER_API_KEY", "environment variable holding the provider credential")
	providerContext := flags.Int("provider-context", 131072, "declared provider context window")
	providerMaxOutput := flags.Int("provider-max-output", 8192, "maximum output tokens for the startup provider")
	// Semantic retrieval is opt-in. An embedding model is a second model to run,
	// and with none configured every path that would use one falls back to the
	// lexical scorer it used before -- a supported configuration, not a degraded
	// one. Turning it on is what lets a goal in one language reach a Skill or an
	// earlier turn written in another (R-14, O-44).
	embedBaseURL := flags.String("embed-url", "",
		"optional OpenAI-compatible embeddings endpoint, for example http://127.0.0.1:11434/v1; "+
			"empty leaves retrieval lexical")
	embedModel := flags.String("embed-model", "bge-m3", "embedding model ID")
	embedAPIKeyEnv := flags.String("embed-api-key-env", "HERMETRIX_EMBED_API_KEY",
		"environment variable holding the embeddings credential; the name is configured, never the value")
	embedDimensions := flags.Int("embed-dimensions", 0,
		"expected vector width; 0 accepts whatever the model returns, any other value rejects a mismatch")
	autoOpen := flags.Bool("open", false,
		"open the control center in the default browser once the listener is up")
	desktopMode := flags.Bool("desktop", false,
		"open the control center in its own application window using an installed "+
			"Chromium-family browser; falls back to --open behaviour when none is found")
	_ = flags.Parse(args)
	if err := requireLoopbackListener(*listen); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	level := slog.LevelInfo
	if *debug {
		level = slog.LevelDebug
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	dataStore, err := store.Open(ctx, *dataRoot)
	if err != nil {
		logger.Error("open data store", "error", err)
		os.Exit(1)
	}
	defer dataStore.Close()
	estimator := ctxcompiler.NewAdaptiveEstimator()
	compiler := ctxcompiler.NewCompiler(estimator, ctxcompiler.NewBlobSpiller(dataStore.Blobs),
		ctxcompiler.NewVerifiedCompactor(ctxcompiler.StructuredCompactor{}))
	skillService := skills.NewService(dataStore)
	gate := runtime.NewInferenceGate()
	learningService := learning.NewService(dataStore, skillService, gate, learning.StructuredReviewer{})
	curatorService := curator.NewService(dataStore, skillService)
	productService := product.NewService(dataStore, skillService)
	defer productService.Close()
	if recovered, recoverErr := productService.RecoverInterruptedJobs(ctx); recoverErr != nil {
		logger.Error("recover background jobs", "error", recoverErr)
		os.Exit(1)
	} else if recovered > 0 {
		logger.Warn("marked interrupted background jobs without retrying side effects", "count", recovered)
	}
	workspaceProject, err := productService.EnsureWorkspaceProject(ctx, *workspace)
	if err != nil {
		logger.Error("register workspace project", "error", err)
		os.Exit(1)
	}
	logger.Info("workspace project ready", "project", workspaceProject.Name, "root", workspaceProject.RootPath)
	// The project the process was started for belongs at the top of the picker.
	// Pinning is a convenience, so a failure here is reported and stepped over:
	// a server that refuses to start because a preference did not save would be
	// trading the whole product for the ordering of one list.
	if _, pinErr := productService.PinProject(ctx, workspaceProject.ID, true); pinErr != nil {
		logger.Warn("pin workspace project", "error", pinErr, "project", workspaceProject.Name)
	}
	fidelityService := fidelity.NewService(dataStore, compiler)
	if err := fidelityService.EnsureDefaultCorpus(ctx); err != nil {
		logger.Error("seed context fidelity corpus", "error", err)
		os.Exit(1)
	}
	// One vault for both provider and MCP credentials, so a token typed into
	// the control center is usable without an environment variable or a
	// restart. Environment variables still work and are still checked.
	vault, err := secrets.Open(*dataRoot)
	if err != nil {
		logger.Error("open credential vault", "error", err)
		os.Exit(1)
	}
	providerService := providers.NewService(dataStore, nil).WithVault(vault)
	localProber := localmodel.NewProber()
	qualificationService := qualification.NewService(dataStore, providerService, localProber, gate, estimator)
	capabilityCatalog := capabilities.NewCatalog()
	mcpService := mcp.NewService(dataStore, capabilityCatalog, nil).WithVault(vault)
	defer mcpService.Close()
	if err := mcpService.ReloadCatalog(ctx); err != nil {
		logger.Error("load MCP capability catalog", "error", err)
		os.Exit(1)
	}
	if *providerBaseURL != "" || *providerModel != "" || *providerName != "" {
		if *providerBaseURL == "" || *providerModel == "" {
			logger.Error("startup provider requires both --provider-base-url and --provider-model")
			os.Exit(2)
		}
		name := *providerName
		if name == "" {
			name = "Startup provider"
		}
		profile, providerErr := providerService.EnsureByName(ctx, providers.SaveInput{Name: name,
			AdapterKind: providers.AdapterOpenAICompatible, BaseURL: *providerBaseURL, Model: *providerModel,
			APIKeyEnv: *providerAPIKeyEnv, ContextWindow: *providerContext, ContextEvidence: "declared",
			MaxOutputTokens: *providerMaxOutput})
		if providerErr != nil {
			logger.Error("configure startup provider", "error", providerErr)
			os.Exit(1)
		}
		logger.Info("provider profile ready", "provider", profile.Name, "model", profile.Model,
			"context", profile.ContextWindow, "credential_ready", profile.CredentialReady)
	}

	// Point the learner at a provider once one exists. Reviews queue from the
	// first turn, so the learning service is built before any provider is, and
	// the startup reviewer can only acknowledge a procedure someone else wrote.
	if reviewProvider, reviewErr := providerService.FirstEnabled(ctx); reviewErr == nil {
		learningService.WithReviewer(learning.NewModelReviewer(providerService, reviewProvider.ID))
		logger.Info("review worker ready", "reviewer", learningService.ReviewerRevision(),
			"provider", reviewProvider.Name, "model", reviewProvider.Model)
	} else {
		logger.Warn("no enabled provider; reviews cannot propose Skills from evidence",
			"reviewer", learningService.ReviewerRevision())
	}
	toolRegistry, err := toolruntime.NewRegistry(*workspace)
	if err != nil {
		logger.Error("configure tool registry", "error", err)
		os.Exit(1)
	}
	toolRegistry.SetCatalog(capabilityCatalog)
	runtimeBridge := agentRuntimeBridge{product: productService}
	agentService := agent.NewService(dataStore, providerService, compiler, estimator, gate, toolRegistry, skillService).
		WithLearning(learningService).WithRuntime(runtimeBridge, runtimeBridge)
	productService.WithAgentRunner(agentService)
	// An MCP server may ask the client to sample a model or to ask the user a
	// question. Only the agent service can do either, so it answers those
	// requests; the MCP client holds the interface, never the implementation.
	mcpService.WithRequestHandler(agentService.NewMCPBridge())
	if *embedBaseURL != "" {
		agentService.SetEmbedder(embedding.NewOpenAIEmbedder(nil, *embedBaseURL, *embedModel,
			os.Getenv(*embedAPIKeyEnv), *embedDimensions))
		logger.Info("semantic retrieval enabled", "model", *embedModel, "endpoint", *embedBaseURL)
	}
	if recovered, recoverErr := agentService.RecoverInterruptedApprovals(ctx); recoverErr != nil {
		logger.Error("recover interrupted tool approvals", "error", recoverErr)
		os.Exit(1)
	} else if recovered > 0 {
		logger.Warn("marked interrupted effects uncertain; inspect affected systems before retry", "count", recovered)
	}
	if recovered, recoverErr := agentService.RecoverInterruptedTurns(ctx); recoverErr != nil {
		logger.Error("recover interrupted agent turns", "error", recoverErr)
		os.Exit(1)
	} else if recovered > 0 {
		logger.Warn("closed interrupted turns without retrying model requests", "count", recovered)
	}
	if recovered, recoverErr := learningService.RecoverInterrupted(ctx); recoverErr != nil {
		logger.Error("recover interrupted learning reviews", "error", recoverErr)
		os.Exit(1)
	} else if recovered > 0 {
		logger.Info("requeued interrupted learning reviews", "count", recovered)
	}
	server := &http.Server{Addr: *listen, Handler: web.New(skillService, learningService, curatorService, compiler, estimator,
		localProber, providerService, agentService, dataStore, logger).WithMCP(mcpService, capabilityCatalog).
		WithFidelity(fidelityService).WithQualification(qualificationService).WithProduct(productService).Handler(),
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 15 * time.Minute,
		IdleTimeout: 120 * time.Second, MaxHeaderBytes: 1 << 20}
	go func() {
		<-ctx.Done()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		_ = server.Shutdown(shutdownCtx)
	}()
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if processed, drainErr := learningService.DrainPending(ctx, 20); drainErr != nil {
					logger.Warn("drain committed learning triggers", "error", drainErr)
				} else if processed > 0 {
					logger.Info("queued committed learning reviews", "count", processed)
				}
				state := curator.DetectSystemState(ctx)
				executions, runErr := curatorService.RunDue(ctx, state)
				if runErr != nil {
					logger.Warn("run scheduled maintenance", "error", runErr)
				}
				for _, execution := range executions {
					if execution.Skipped == "" {
						logger.Info("scheduled maintenance completed", "task", execution.TaskKind,
							"result", execution.ResultID, "error", execution.Error)
					}
				}
			}
		}
	}()
	listener, err := net.Listen("tcp", *listen)
	if err != nil {
		logger.Error("serve", "error", err)
		os.Exit(1)
	}
	url := "http://" + *listen
	logger.Info("Hermetrix Skill Control Center", "url", url, "data", *dataRoot)
	if *desktopMode || *autoOpen {
		// Only after the bind has succeeded, and never fatal: the server is the
		// point, the browser is a convenience.
		profileDir := filepath.Join(*dataRoot, "desktop-profile")
		go func() {
			if *desktopMode {
				openErr := openDesktopWindow(url, profileDir)
				if openErr == nil {
					logger.Info("desktop window opened", "url", url, "profile", profileDir)
					return
				}
				// A missing browser is the expected reason to fall through; any
				// other failure is reported before the fallback so the user is
				// not left wondering why the window looks like a browser tab.
				if errors.Is(openErr, errNoDesktopBrowser) {
					logger.Warn("desktop mode unavailable; opening the default browser instead",
						"reason", openErr)
				} else {
					logger.Warn("open desktop window", "error", openErr, "url", url)
				}
			}
			if openErr := openBrowser(url); openErr != nil {
				logger.Warn("open browser", "error", openErr, "url", url)
			}
		}()
	}
	if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
		logger.Error("serve", "error", err)
		os.Exit(1)
	}
}

func requireLoopbackListener(address string) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("invalid --listen address: %w", err)
	}
	if host == "localhost" {
		return nil
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("refusing non-loopback --listen %q while the local control API has no authentication", address)
	}
	return nil
}
