package main

import (
	"context"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/zcw199604/wecom-home-ops/internal/app"
	"github.com/zcw199604/wecom-home-ops/internal/config"
	"github.com/zcw199604/wecom-home-ops/internal/wecom"
)

func main() {
	var configPath string
	var wecomSyncMenu bool
	flag.StringVar(&configPath, "config", "config.yaml", "配置文件路径（YAML）")
	flag.BoolVar(&wecomSyncMenu, "wecom-sync-menu", false, "同步企业微信应用自定义菜单（menu/create）后退出")
	flag.Parse()

	bootstrapLogger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(bootstrapLogger)

	cfg, err := config.Load(configPath)
	if err != nil {
		slog.Error("加载配置失败", "path", configPath, "error", err)
		os.Exit(1)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: cfg.Log.Level.ToSlogLevel(),
	}))
	slog.SetDefault(logger)

	if wecomSyncMenu {
		httpClient := &http.Client{
			Timeout: cfg.Server.HTTPClientTimeout.ToDuration(),
		}
		wecomClient := wecom.NewClient(wecom.ClientConfig{
			APIBaseURL: cfg.WeCom.APIBaseURL,
			CorpID:     cfg.WeCom.CorpID,
			AgentID:    cfg.WeCom.AgentID,
			Secret:     cfg.WeCom.Secret,
		}, httpClient)

		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		if err := wecomClient.CreateMenu(ctx, wecom.DefaultMenu()); err != nil {
			slog.Error("企业微信自定义菜单同步失败", "error", err)
			os.Exit(1)
		}
		slog.Info("企业微信自定义菜单同步成功")
		return
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	server, err := app.NewServer(cfg)
	if err != nil {
		slog.Error("初始化失败", "error", err)
		os.Exit(1)
	}

	go func() {
		if err := server.Start(); err != nil {
			slog.Error("HTTP 服务启动失败", "error", err)
			stop()
		}
	}()

	<-ctx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		slog.Error("HTTP 服务关闭失败", "error", err)
	}
}
