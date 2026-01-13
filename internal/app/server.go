package app

// server.go 负责装配依赖并启动 HTTP 路由（企业微信回调入口等）。
import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/zcw199604/wecom-home-ops/internal/config"
	"github.com/zcw199604/wecom-home-ops/internal/core"
	"github.com/zcw199604/wecom-home-ops/internal/qinglong"
	"github.com/zcw199604/wecom-home-ops/internal/unraid"
	"github.com/zcw199604/wecom-home-ops/internal/wecom"
)

type Server struct {
	cfg        config.Config
	server     *http.Server
	stateStore *core.StateStore
	deduper    *wecom.Deduper
}

func NewServer(cfg config.Config) (*Server, error) {
	httpClient := &http.Client{
		Timeout: cfg.Server.HTTPClientTimeout.ToDuration(),
	}

	wecomClient := wecom.NewClient(wecom.ClientConfig{
		APIBaseURL: cfg.WeCom.APIBaseURL,
		CorpID:     cfg.WeCom.CorpID,
		AgentID:    cfg.WeCom.AgentID,
		Secret:     cfg.WeCom.Secret,
	}, httpClient)

	stateStore := core.NewStateStore(cfg.Core.StateTTL.ToDuration())
	wecomSender := core.NewTemplateCardSender(core.TemplateCardSenderDeps{
		Base:  wecomClient,
		State: stateStore,
		Mode:  core.TemplateCardMode(cfg.WeCom.TemplateCardMode),
	})
	deduper := wecom.NewDeduper(10 * time.Minute)

	var providers []core.ServiceProvider

	if cfg.Unraid.Endpoint != "" && cfg.Unraid.APIKey != "" {
		unraidClient := unraid.NewClient(unraid.ClientConfig{
			Endpoint: cfg.Unraid.Endpoint,
			APIKey:   cfg.Unraid.APIKey,
			Origin:   cfg.Unraid.Origin,

			LogsField:        cfg.Unraid.LogsField,
			LogsTailArg:      cfg.Unraid.LogsTailArg,
			LogsPayloadField: cfg.Unraid.LogsPayloadField,

			StatsField:  cfg.Unraid.StatsField,
			StatsFields: cfg.Unraid.StatsFields,

			ForceUpdateMutation:     cfg.Unraid.ForceUpdateMutation,
			ForceUpdateArgName:      cfg.Unraid.ForceUpdateArgName,
			ForceUpdateArgType:      cfg.Unraid.ForceUpdateArgType,
			ForceUpdateReturnFields: cfg.Unraid.ForceUpdateReturnFields,
		}, httpClient)
		providers = append(providers, unraid.NewProvider(unraid.ProviderDeps{
			WeCom:  wecomSender,
			Client: unraidClient,
			State:  stateStore,
		}))
	}

	if len(cfg.Qinglong.Instances) > 0 {
		var instances []qinglong.Instance
		for _, ins := range cfg.Qinglong.Instances {
			client, err := qinglong.NewClient(qinglong.ClientConfig{
				BaseURL:      ins.BaseURL,
				ClientID:     ins.ClientID,
				ClientSecret: ins.ClientSecret,
			}, httpClient)
			if err != nil {
				return nil, err
			}
			instances = append(instances, qinglong.Instance{
				ID:     ins.ID,
				Name:   ins.Name,
				Client: client,
			})
		}
		providers = append(providers, qinglong.NewProvider(qinglong.ProviderDeps{
			WeCom:     wecomSender,
			State:     stateStore,
			Instances: instances,
		}))
	}

	router := core.NewRouter(core.RouterDeps{
		WeCom:         wecomSender,
		AllowedUserID: make(map[string]struct{}),
		Providers:     providers,
		State:         stateStore,
	})
	for _, id := range cfg.Auth.AllowedUserIDs {
		router.AllowedUserID[id] = struct{}{}
	}

	crypto, err := wecom.NewCrypto(wecom.CryptoConfig{
		Token:          cfg.WeCom.Token,
		EncodingAESKey: cfg.WeCom.EncodingAESKey,
		ReceiverID:     cfg.WeCom.CorpID,
	})
	if err != nil {
		return nil, err
	}

	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	mux.Handle("GET /wecom/callback", wecom.NewCallbackVerifyHandler(crypto))
	mux.Handle("POST /wecom/callback", wecom.NewCallbackHandler(wecom.CallbackDeps{
		Crypto:  crypto,
		Core:    router,
		Deduper: deduper,
	}))

	s := &http.Server{
		Addr:              cfg.Server.ListenAddr,
		Handler:           withRequestLogging(mux),
		ReadHeaderTimeout: cfg.Server.ReadHeaderTimeout.ToDuration(),
	}

	return &Server{
		cfg:        cfg,
		server:     s,
		stateStore: stateStore,
		deduper:    deduper,
	}, nil
}

func (s *Server) Start() error {
	listener, err := net.Listen("tcp", s.cfg.Server.ListenAddr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	return s.Serve(listener)
}

func (s *Server) Serve(listener net.Listener) error {
	if listener == nil {
		return fmt.Errorf("nil listener")
	}
	slog.Info("HTTP 服务启动",
		"listen_addr", s.cfg.Server.ListenAddr,
		"listener_addr", listener.Addr().String(),
	)
	err := s.server.Serve(listener)
	if err == nil || errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return fmt.Errorf("serve: %w", err)
}

func (s *Server) Shutdown(ctx context.Context) error {
	slog.Info("HTTP 服务关闭中")
	err := s.server.Shutdown(ctx)
	if s.stateStore != nil {
		s.stateStore.Close()
	}
	if s.deduper != nil {
		s.deduper.Close()
	}
	return err
}

func withRequestLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w}
		next.ServeHTTP(sw, r)
		if sw.status == 0 {
			sw.status = http.StatusOK
		}
		slog.Info("请求完成",
			"method", r.Method,
			"path", r.URL.Path,
			"status_code", sw.status,
			"response_bytes", sw.bytes,
			"duration_ms", time.Since(start).Milliseconds(),
		)
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (w *statusWriter) WriteHeader(statusCode int) {
	w.status = statusCode
	w.ResponseWriter.WriteHeader(statusCode)
}

func (w *statusWriter) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(p)
	w.bytes += n
	return n, err
}
