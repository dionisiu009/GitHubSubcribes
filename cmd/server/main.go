package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/dionisiu009/GitHubSubcribes/internal/api"
	"github.com/dionisiu009/GitHubSubcribes/internal/config"
	"github.com/dionisiu009/GitHubSubcribes/internal/notifier"
	"github.com/dionisiu009/GitHubSubcribes/internal/repository"
	"github.com/dionisiu009/GitHubSubcribes/internal/scanner"
	"github.com/dionisiu009/GitHubSubcribes/internal/service"
	"github.com/dionisiu009/GitHubSubcribes/pkg/logger"
	"github.com/dionisiu009/GitHubSubcribes/pkg/redisclient"
)

func main() {
	// -------------------------------------------------------------------------
	// 1. Ранній логер (до читання конфігу — рівень debug за замовчуванням)
	// -------------------------------------------------------------------------
	log := logger.New("debug", "development")
	slog.SetDefault(log)

	if err := run(log); err != nil {
		log.Error("критична помилка запуску", "err", err)
		os.Exit(1)
	}
}

// run містить всю логіку ініціалізації та запуску — зручно для тестування
func run(log *slog.Logger) error {
	// -------------------------------------------------------------------------
	// 2. Конфігурація
	// -------------------------------------------------------------------------
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("конфіг: %w", err)
	}
	// Переініціалізуємо логер з реальними налаштуваннями з конфігу
	log = logger.New(cfg.App.LogLevel, cfg.App.Env)
	slog.SetDefault(log)
	log.Info("конфігурацію завантажено", "env", cfg.App.Env)

	// -------------------------------------------------------------------------
	// 3. Міграції БД (запускаються до відкриття пулу з'єднань)
	// -------------------------------------------------------------------------
	log.Info("застосовуємо міграції БД...")
	if err = repository.RunMigrations(cfg.DB, cfg.App.MigrationPath); err != nil {
		return fmt.Errorf("міграції: %w", err)
	}
	log.Info("міграції застосовано")

	// -------------------------------------------------------------------------
	// 4. PostgreSQL
	// -------------------------------------------------------------------------
	repo, err := repository.New(cfg.DB)
	if err != nil {
		return fmt.Errorf("postgresql: %w", err)
	}
	defer func() {
		if cerr := repo.Close(); cerr != nil {
			log.Error("помилка закриття БД", "err", cerr)
		}
	}()
	log.Info("підключення до PostgreSQL встановлено")

	// -------------------------------------------------------------------------
	// 5. Redis
	// -------------------------------------------------------------------------
	rdb, err := redisclient.New(cfg.Redis)
	if err != nil {
		return fmt.Errorf("redis: %w", err)
	}
	defer rdb.Close()
	log.Info("підключення до Redis встановлено")

	// -------------------------------------------------------------------------
	// 6. Ініціалізація шарів
	// -------------------------------------------------------------------------
	ghClient := scanner.NewGitHubClient(cfg.GitHub, cfg.Redis, rdb)
	emailNotifier := notifier.New(cfg.SMTP, cfg.App.BaseURL)
	svc := service.New(repo, ghClient, emailNotifier)
	handler := api.NewHandler(svc)
	router := api.NewRouter(handler)

	// -------------------------------------------------------------------------
	// 7. Фоновий сканер (запускається в окремій горутині)
	// -------------------------------------------------------------------------
	// ctx з cancel для graceful-зупинки сканера разом з сервером
	scanCtx, scanCancel := context.WithCancel(context.Background())
	defer scanCancel()

	sc := scanner.NewScanner(repo, ghClient, emailNotifier, cfg.GitHub.ScanInterval, log)
	go sc.Run(scanCtx)

	// -------------------------------------------------------------------------
	// 8. HTTP сервер
	// -------------------------------------------------------------------------
	srv := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.HTTP.Port),
		Handler:      router,
		ReadTimeout:  cfg.HTTP.ReadTimeout,
		WriteTimeout: cfg.HTTP.WriteTimeout,
		IdleTimeout:  cfg.HTTP.IdleTimeout,
	}

	// Слухаємо SIGINT / SIGTERM для graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	// Запускаємо сервер у горутині
	serverErr := make(chan error, 1)
	go func() {
		log.Info("HTTP сервер запущено", "addr", srv.Addr)
		if err = srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serverErr <- err
		}
	}()

	// Чекаємо або сигнал зупинки, або критичну помилку сервера
	select {
	case sig := <-quit:
		log.Info("отримано сигнал зупинки", "signal", sig.String())
	case err = <-serverErr:
		return fmt.Errorf("http server: %w", err)
	}

	// -------------------------------------------------------------------------
	// 9. Graceful shutdown — даємо 15 секунд на завершення активних запитів
	// -------------------------------------------------------------------------
	scanCancel() // зупиня

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer shutdownCancel()

	log.Info("зупиняємо HTTP сервер...")
	if err = srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("graceful shutdown: %w", err)
	}

	log.Info("сервер зупинено успішно")
	return nil
}
