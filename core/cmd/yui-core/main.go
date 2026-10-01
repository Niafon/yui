// Command yui-core is the single long-lived process that owns sessions,
// memory, personalities, permissions and providers (CORE-001).
//
// Heavy ML never runs inside this process: it is delegated to workers over the
// provider contracts (ADR-003, ADR-005).
package main

import (
	"context"
	"errors"
	"flag"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/yui-companion/core/internal/agent"
	"github.com/yui-companion/core/internal/api"
	"github.com/yui-companion/core/internal/audit"
	"github.com/yui-companion/core/internal/config"
	"github.com/yui-companion/core/internal/crypto"
	"github.com/yui-companion/core/internal/eventbus"
	"github.com/yui-companion/core/internal/identity"
	"github.com/yui-companion/core/internal/ids"
	"github.com/yui-companion/core/internal/inference"
	"github.com/yui-companion/core/internal/logging"
	"github.com/yui-companion/core/internal/memory"
	"github.com/yui-companion/core/internal/model"
	"github.com/yui-companion/core/internal/modelsettings"
	"github.com/yui-companion/core/internal/permission"
	"github.com/yui-companion/core/internal/provider"
	"github.com/yui-companion/core/internal/scheduler"
	"github.com/yui-companion/core/internal/session"
	"github.com/yui-companion/core/internal/store"
	"github.com/yui-companion/core/internal/store/memstore"
	"github.com/yui-companion/core/internal/store/postgres"
	"github.com/yui-companion/core/internal/store/sqlite"
	"github.com/yui-companion/core/internal/supervisor"
	"github.com/yui-companion/core/internal/tools"
)

const ownerUserID = "user_owner"

func main() {
	configPath := flag.String("config", "yui.config.json", "path to the configuration file")
	printToken := flag.Bool("print-token", true, "print the loopback token for the desktop stage")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		panic("configuration error: " + err.Error())
	}
	modelSettings, err := modelsettings.Open(filepath.Join(cfg.DataDir, "model-settings.json"))
	if err != nil {
		panic("model settings error: " + err.Error())
	}
	savedModels := modelSettings.Snapshot()
	cfg.Providers = append(cfg.Providers, savedModels.Providers...)
	if cfg.Defaults == nil {
		cfg.Defaults = map[string]string{}
	}
	for kind, id := range savedModels.Defaults {
		cfg.Defaults[kind] = id
	}
	log := logging.Setup(cfg.Logging.Level, cfg.Logging.JSON)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if cfg.Server.LoopbackToken == "" {
		cfg.Server.LoopbackToken = ids.Token()
		if *printToken {
			log.Info("desktop stage token generated", "token", cfg.Server.LoopbackToken)
		}
	}

	st, err := openStore(ctx, cfg)
	if err != nil {
		log.Error("storage unavailable", "error", err)
		os.Exit(1)
	}
	defer st.Close()

	bus := eventbus.New()
	auditSvc := audit.New(st.Audit(), bus)
	perms := permission.New(st.Permissions(), cfg.Privacy.LocalAllowedCategories)

	registry := provider.NewRegistry()
	if err := registry.Build(cfg.Providers, cfg.Defaults); err != nil {
		log.Error("provider configuration rejected", "error", err)
		os.Exit(1)
	}

	// Encryption is initialised before anything can write (ADR-024/025). The
	// password comes from the environment for an unattended start; the desktop
	// launcher prompts for it instead.
	if cfg.Crypto.Enabled {
		keys := crypto.New(cfg.Crypto.KeyFile, crypto.Params{
			Time: cfg.Crypto.Argon2Time, MemoryKB: cfg.Crypto.Argon2MemoryMB * 1024,
			Threads: cfg.Crypto.Argon2Threads,
		})
		password := os.Getenv("YUI_PASSWORD")
		switch {
		case password == "":
			log.Warn("encryption enabled but YUI_PASSWORD is not set; sensitive fields stay locked")
		default:
			if err := keys.Unlock(password); err == crypto.ErrNoKeyFile {
				recovery, err := keys.Initialise(password)
				if err != nil {
					log.Error("cannot create key file", "error", err)
					os.Exit(1)
				}
				// Printed exactly once, never stored by the core.
				log.Warn("write down the recovery key; it is the only way in without the password",
					"recovery_key", recovery)
			} else if err != nil {
				log.Error("cannot unlock key file", "error", err)
				os.Exit(1)
			}
			if keys.NeedsRotation() {
				log.Warn("encryption key has been used enough times to warrant rotation")
			}
		}
	}

	memSvc := memory.New(st.Memory(), perms, auditSvc, registry, bus, cfg.Memory)
	extractor := memory.NewExtractor(registry)
	identSvc := identity.New(st.Identities(), auditSvc)
	sessions := session.NewManager(st.Sessions(), bus)
	// Tools: the MVP set from ADR-026. media and desktop capabilities are nil
	// on a bare install, so those tools report honestly that they cannot act.
	toolRegistry := tools.NewRegistry()
	workspace := tools.NewWorkspace()
	if err := tools.RegisterBuiltins(toolRegistry, workspace, memorySearch{memSvc}, nil); err != nil {
		log.Error("cannot register tools", "error", err)
		os.Exit(1)
	}
	if err := tools.RegisterProcedureLearning(toolRegistry, memSvc); err != nil {
		log.Error("cannot register procedural memory", "error", err)
		os.Exit(1)
	}
	if cfg.WebSearch.Enabled {
		if err := tools.RegisterWebSearch(toolRegistry); err != nil {
			log.Error("cannot register web search", "error", err)
			os.Exit(1)
		}
	}

	runtime := agent.NewRuntime(registry, perms, auditSvc, memSvc, extractor, identSvc, sessions, toolRegistry)

	self, err := identSvc.EnsureDefault(ctx, ownerUserID)
	if err != nil {
		log.Error("cannot initialise personality", "error", err)
		os.Exit(1)
	}
	if err := memSvc.EnsureSpaces(ctx, ownerUserID, self.ID); err != nil {
		log.Warn("memory spaces already present or partially created", "error", err)
	}
	log.Info("personality ready", "identity", self.Name, "id", self.ID, "mode", self.Mode)

	sup := supervisor.New()
	for _, spec := range cfg.Workers {
		sup.Add(spec)
	}
	sup.StartAll(ctx)
	defer sup.StopAll()

	var inferenceMgr *inference.Manager
	if cfg.Inference.Enabled {
		monitor := inference.NewMonitor(cfg.Inference)
		monitor.Start(ctx)
		inferenceMgr = inference.New(cfg.Inference, registry, monitor, sup, ctx)
		for kind, id := range savedModels.Defaults {
			inferenceMgr.SetExplicitDefault(model.ProviderKind(kind), id)
		}
		if savedModels.Preferences != nil {
			if err := inferenceMgr.UpdatePreferences(*savedModels.Preferences); err != nil {
				log.Error("saved model preferences rejected", "error", err)
				os.Exit(1)
			}
		}
		runtime.SetInferenceManager(inferenceMgr)
		log.Info("adaptive inference enabled", "mode", cfg.Inference.Mode,
			"gpu_limit", cfg.Inference.MaxGPUUtilPercent, "vram_reserve_mb", cfg.Inference.VRAMReserveMB)
	}

	sched := scheduler.New(cfg.Scheduler, sessions.Busy)
	if cfg.Computer.Enabled {
		err := tools.RegisterComputer(toolRegistry, cfg.Computer.Endpoint, func(callCtx context.Context) error {
			if inferenceMgr == nil {
				return errors.New("computer: inference manager is disabled")
			}
			if err := inferenceMgr.PrepareWorker(callCtx, cfg.Computer.ModelWorkerID); err != nil {
				return err
			}
			return inferenceMgr.PrepareWorker(callCtx, cfg.Computer.WorkerID)
		})
		if err != nil {
			log.Error("cannot register computer tools", "error", err)
			os.Exit(1)
		}
	}
	registerJobs(sched, memSvc, extractor, sessions, identSvc, self.ID)
	registerReminderJob(sched, workspace, sessions)
	if cfg.Diagnostics.Enabled {
		// Leak detection is a background job like any other: it pauses while
		// the owner is talking (NFR-006).
		interval := time.Duration(cfg.Diagnostics.LeakCheckMinutes) * time.Minute
		if interval <= 0 {
			interval = 30 * time.Minute
		}
		sched.Add(scheduler.Job{Name: "runtime.leaks", Interval: interval, Run: api.WatchGoroutineLeaks})
	}
	sched.Start(ctx)
	defer sched.Stop()

	srv := api.New(api.Deps{
		Cfg: cfg, Store: st, Bus: bus, Sessions: sessions, Agent: runtime,
		Memory: memSvc, Identities: identSvc, Perms: perms, Audit: auditSvc,
		Providers: registry, Scheduler: sched, Supervisor: sup, Tools: toolRegistry,
		Inference: inferenceMgr, ModelSettings: modelSettings,
		UserID: ownerUserID,
	})

	httpSrv := &http.Server{
		Addr:              cfg.Server.Addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		// Bounds on what a client may send before it is authenticated. The
		// core listens on a home network, where a compromised smart device is
		// a more realistic attacker than the internet.
		MaxHeaderBytes: 1 << 16,
		TLSConfig:      api.TLSConfig(),
		// No write timeout: websocket streams are long lived by design.
	}
	configureServerLimits(httpSrv)

	go func() {
		log.Info("yui core listening", "addr", cfg.Server.Addr, "store", cfg.Database.Driver)
		var err error
		if cfg.Server.TLSCertFile != "" && cfg.Server.TLSKeyFile != "" {
			err = httpSrv.ListenAndServeTLS(cfg.Server.TLSCertFile, cfg.Server.TLSKeyFile)
		} else {
			err = httpSrv.ListenAndServe()
		}
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("http server stopped", "error", err)
			stop()
		}
	}()

	// The PC UI uses a loopback-only listener. Phones use the TLS listener above.
	var localSrv *http.Server
	if cfg.Server.LocalAddr != "" {
		localSrv = &http.Server{Addr: cfg.Server.LocalAddr, Handler: srv.Handler(), ReadHeaderTimeout: 10 * time.Second, MaxHeaderBytes: 1 << 16}
		go func() {
			log.Info("local desktop listening", "addr", cfg.Server.LocalAddr)
			if err := localSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Error("local server stopped", "error", err)
				stop()
			}
		}()
	}

	<-ctx.Done()
	log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = httpSrv.Shutdown(shutdownCtx)
	if localSrv != nil {
		_ = localSrv.Shutdown(shutdownCtx)
	}
}

func openStore(ctx context.Context, cfg *config.Config) (store.Store, error) {
	switch cfg.Database.Driver {
	case "sqlite":
		return sqlite.Open(ctx, cfg.Database.Path)
	case "postgres":
		return postgres.Open(ctx, cfg.Database.DSN)
	default:
		return memstore.Open(cfg.DataDir)
	}
}

// registerJobs installs background reflection and maintenance (SRS 12.4).
func registerJobs(s *scheduler.Scheduler, mem *memory.Service, ext *memory.Extractor, sessions *session.Manager, ident *identity.Service, identityID string) {
	s.Add(scheduler.Job{
		Name:     "memory.retention",
		Interval: time.Hour,
		Run: func(ctx context.Context) error {
			_, err := mem.ApplyRetention(ctx)
			return err
		},
	})

	s.Add(scheduler.Job{
		Name:     "memory.reflection",
		Interval: 15 * time.Minute,
		Run: func(ctx context.Context) error {
			active, err := sessions.ListActive(ctx)
			if err != nil || len(active) == 0 {
				return err
			}
			for _, sess := range active {
				turns, err := sessions.Turns(ctx, sess.ID, 30)
				if err != nil || len(turns) < 4 {
					continue
				}
				transcript := ""
				for _, t := range turns {
					transcript += t.Role + ": " + t.Text + "\n"
				}
				cands, err := ext.FromLLM(ctx, sess.Providers["llm"], sess.IdentityID,
					memory.SpaceUserGeneral, transcript,
					model.Provenance{Kind: "conversation", Ref: sess.ID, At: time.Now().UTC()})
				if err != nil {
					return err
				}
				for _, c := range cands {
					if _, err := mem.Remember(ctx, c); err != nil {
						return err
					}
				}
			}
			return nil
		},
	})

	s.Add(scheduler.Job{
		Name:     "emotion.decay",
		Interval: 30 * time.Minute,
		Run: func(ctx context.Context) error {
			_, err := ident.Apply(ctx, identityID, identity.Delta{Cause: "idle decay"})
			return err
		},
	})
}

// memorySearch adapts the memory service to the narrow interface the tools
// need. A tool sees search results, never the store.
type memorySearch struct{ mem *memory.Service }

func (m memorySearch) SearchText(ctx context.Context, identityID, query string, limit int) ([]string, error) {
	found, err := m.mem.Retrieve(ctx, memory.RetrieveRequest{
		IdentityID: identityID,
		Query:      query,
		SpaceIDs:   []string{memory.SpaceUserProfile, memory.SpaceUserGeneral},
		Limit:      limit,
	})
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(found))
	for _, sc := range found {
		out = append(out, sc.Item.Content)
	}
	return out, nil
}

// registerReminderJob turns due reminders and timers into proactive messages.
// It is the first proactivity trigger (SRS 12.1) and it obeys the same rule as
// every other background job: it yields to an active conversation.
func registerReminderJob(s *scheduler.Scheduler, ws *tools.Workspace, sessions *session.Manager) {
	s.Add(scheduler.Job{
		Name:     "reminders.due",
		Interval: 30 * time.Second,
		Run: func(ctx context.Context) error {
			due := ws.Due(time.Now())
			if len(due) == 0 {
				return nil
			}
			active, err := sessions.ListActive(ctx)
			if err != nil || len(active) == 0 {
				return err
			}
			for _, sess := range active {
				for _, rem := range due {
					sessions.Publish(sess.ID, "proactive.reminder", map[string]any{
						"id": rem.ID, "text": rem.Text, "due_at": rem.DueAt,
						"source": rem.Source,
					})
				}
			}
			return nil
		},
	})
}
