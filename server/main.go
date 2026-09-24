// Command server — бэкенд «Бипланов»: вход через Telegram, сохранение матчей, рейтинг.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

type config struct {
	BotToken      string // токен @BiplanesGameBot
	Listen        string // адрес HTTP-сервера, по умолчанию 127.0.0.1:8093
	DBPath        string // путь к SQLite
	PublicURL     string // https://biplanes.one-way.dev — для webhook и кнопки Mini App
	WebhookSecret string // X-Telegram-Bot-Api-Secret-Token
	StaticDir     string // для локальной разработки: отдавать фронт с этого же сервера
}

type app struct {
	cfg config
	db  *store
	tg  *telegram
	bot string // username бота без @
}

func loadConfig() (config, error) {
	c := config{
		BotToken:      os.Getenv("BOT_TOKEN"),
		Listen:        envOr("LISTEN_ADDR", "127.0.0.1:8093"),
		DBPath:        envOr("DB_PATH", "biplanes.db"),
		PublicURL:     strings.TrimRight(os.Getenv("PUBLIC_URL"), "/"),
		WebhookSecret: os.Getenv("WEBHOOK_SECRET"),
		StaticDir:     os.Getenv("STATIC_DIR"),
	}
	if c.BotToken == "" {
		return c, errors.New("BOT_TOKEN is required")
	}
	if c.PublicURL != "" && len(c.WebhookSecret) < 32 {
		return c, errors.New("WEBHOOK_SECRET (>= 32 chars) is required when PUBLIC_URL is set")
	}
	return c, nil
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	slog.SetDefault(log)
	if err := run(); err != nil {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	db, err := openStore(cfg.DBPath)
	if err != nil {
		return err
	}
	defer db.close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	tg := newTelegram(cfg.BotToken)
	me, err := tg.getMe(ctx)
	if err != nil {
		return err
	}
	a := &app{cfg: cfg, db: db, tg: tg, bot: me.Username}
	slog.Info("bot", "username", a.bot)

	if cfg.PublicURL != "" {
		if err := tg.setWebhook(ctx, cfg.PublicURL+"/tg/webhook", cfg.WebhookSecret); err != nil {
			return err
		}
		if err := tg.setMenuButton(ctx, "Играть", cfg.PublicURL); err != nil {
			slog.Warn("set menu button", "err", err)
		}
	}

	go a.cleanupLoop(ctx)

	srv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           a.routes(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	errc := make(chan error, 1)
	go func() {
		slog.Info("listening", "addr", cfg.Listen)
		errc <- srv.ListenAndServe()
	}()
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}

func (a *app) cleanupLoop(ctx context.Context) {
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	for {
		if err := a.db.cleanup(ctx, time.Now()); err != nil {
			slog.Warn("cleanup", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
