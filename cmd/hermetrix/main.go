package main

import (
	"context"
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
	"hermetrix-harness/internal/fidelity"
	"hermetrix-harness/internal/learning"
	"hermetrix-harness/internal/localmodel"
	"hermetrix-harness/internal/mcp"
	"hermetrix-harness/internal/product"
	"hermetrix-harness/internal/providers"
	"hermetrix-harness/internal/qualification"
	"hermetrix-harness/internal/runtime"
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
	case "corpus":
		if err := runCorpus(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "corpus:", err)
			os.Exit(1)
		}
	default:
		usage()
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage:")
	fmt.Fprintln(os.Stderr, "  hermetrix serve  [--data PATH] [--listen HOST:PORT]")
	fmt.Fprintln(os.Stderr, "  hermetrix corpus export --data PATH --out DIR")
	fmt.Fprintln(os.Stderr, "  hermetrix corpus score  --data PATH --dir DIR [--provider NAME]")
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
	fidelityService := fidelity.NewService(dataStore, compiler)
	if err := fidelityService.EnsureDefaultCorpus(ctx); err != nil {
		logger.Error("seed context fidelity corpus", "error", err)
		os.Exit(1)
	}
	providerService := providers.NewService(dataStore, nil)
	localProber := localmodel.NewProber()
	qualificationService := qualification.NewService(dataStore, providerService, localProber, gate, estimator)
	capabilityCatalog := capabilities.NewCatalog()
	mcpService := mcp.NewService(dataStore, capabilityCatalog, nil)
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
	agentService := agent.NewService(dataStore, providerService, compiler, estimator, gate, toolRegistry, skillService).WithLearning(learningService)
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
	logger.Info("Hermetrix Skill Control Center", "url", "http://"+*listen, "data", *dataRoot)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
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
