package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres" // postgres driver
	_ "github.com/golang-migrate/migrate/v4/source/file"       // file source
	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq" // postgres dialect для database/sql

	"github.com/dionisiu009/GitHubSubcribes/internal/config"
	"github.com/dionisiu009/GitHubSubcribes/internal/models"
)

// Repository — контракт для роботи з БД.
// Усі методи отримують context для підтримки дедлайнів та скасування.
type Repository interface {
	// --- subscribers ---
	// GetOrCreateSubscriber повертає існуючого підписника або створює нового
	GetOrCreateSubscriber(ctx context.Context, email string) (*models.Subscriber, error)

	// --- repositories ---
	// GetOrCreateRepository повертає існуючий репозиторій або створює новий
	GetOrCreateRepository(ctx context.Context, name string) (*models.Repository, error)
	// UpdateLastSeenTag оновлює тег останнього релізу (викликається після розсилки)
	UpdateLastSeenTag(ctx context.Context, repoName, tag string) error

	// --- subscriptions ---
	// CreateSubscription створює новий запис підписки (confirmed=false)
	CreateSubscription(ctx context.Context, subscriberID, repositoryID int64, token string) error
	// FindSubscriptionByToken шукає підписку за токеном
	FindSubscriptionByToken(ctx context.Context, token string) (*models.Subscription, error)
	// ConfirmSubscription виставляє confirmed=true для заданого токена
	ConfirmSubscription(ctx context.Context, token string) error
	// DeleteSubscriptionByToken видаляє підписку (відписка)
	DeleteSubscriptionByToken(ctx context.Context, token string) error
	// GetSubscriptionsByEmail повертає всі підписки для вказаного email
	GetSubscriptionsByEmail(ctx context.Context, email string) ([]models.SubscriptionResponse, error)
	// IsAlreadySubscribed перевіряє чи підписка вже існує
	IsAlreadySubscribed(ctx context.Context, subscriberID, repositoryID int64) (bool, error)

	// --- view ---
	// GetActiveSubscribersByRepo повертає підписників репо з active_subscribers_view
	GetActiveSubscribersByRepo(ctx context.Context, repoName string) ([]models.ActiveSubscriber, error)
	// GetAllTrackedRepos повертає унікальні назви репо з активними підписками (для сканера)
	GetAllTrackedRepos(ctx context.Context) ([]string, error)
}

// DB — конкретна реалізація Repository поверх sqlx + PostgreSQL
type DB struct {
	pool *sqlx.DB
}

// New відкриває пул з'єднань та перевіряє доступність БД
func New(cfg config.DBConfig) (*DB, error) {
	pool, err := sqlx.Open("postgres", cfg.DSN())
	if err != nil {
		return nil, fmt.Errorf("repository: open db: %w", err)
	}

	pool.SetMaxOpenConns(cfg.MaxOpenConns)
	pool.SetMaxIdleConns(cfg.MaxIdleConns)
	pool.SetConnMaxLifetime(cfg.ConnMaxLifetime)

	if err = pool.Ping(); err != nil {
		return nil, fmt.Errorf("repository: ping db: %w", err)
	}

	return &DB{pool: pool}, nil
}

// Close закриває пул з'єднань
func (d *DB) Close() error {
	return d.pool.Close()
}

// RunMigrations застосовує всі нові up-міграції при старті застосунку.
// Якщо міграцій немає — це не помилка.
func RunMigrations(cfg config.DBConfig, migrationsPath string) error {
	// golang-migrate очікує postgres DSN у форматі URL
	dsn := fmt.Sprintf(
		"postgres://%s:%s@%s:%d/%s?sslmode=%s",
		cfg.User, cfg.Password, cfg.Host, cfg.Port, cfg.Name, cfg.SSLMode,
	)

	m, err := migrate.New("file://"+migrationsPath, dsn)
	if err != nil {
		return fmt.Errorf("repository: create migrator: %w", err)
	}
	defer m.Close()

	if err = m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("repository: run migrations: %w", err)
	}

	return nil
}

// =============================================================================
// Реалізація методів Repository
// =============================================================================

// GetOrCreateSubscriber повертає існуючого або вставляє нового підписника
func (d *DB) GetOrCreateSubscriber(ctx context.Context, email string) (*models.Subscriber, error) {
	var sub models.Subscriber

	// INSERT ... ON CONFLICT — атомарна upsert операція
	const q = `
		INSERT INTO subscribers (email)
		VALUES ($1)
		ON CONFLICT (email) DO UPDATE SET email = EXCLUDED.email
		RETURNING id, email, created_at`

	if err := d.pool.GetContext(ctx, &sub, q, email); err != nil {
		return nil, fmt.Errorf("repository: get_or_create subscriber: %w", err)
	}
	return &sub, nil
}

// GetOrCreateRepository повертає існуючий або вставляє новий репозиторій
func (d *DB) GetOrCreateRepository(ctx context.Context, name string) (*models.Repository, error) {
	var repo models.Repository

	const q = `
		INSERT INTO repositories (name)
		VALUES ($1)
		ON CONFLICT (name) DO UPDATE SET name = EXCLUDED.name
		RETURNING id, name, last_seen_tag`

	if err := d.pool.GetContext(ctx, &repo, q, name); err != nil {
		return nil, fmt.Errorf("repository: get_or_create repository: %w", err)
	}
	return &repo, nil
}

// UpdateLastSeenTag оновлює last_seen_tag після розсилки
func (d *DB) UpdateLastSeenTag(ctx context.Context, repoName, tag string) error {
	const q = `UPDATE repositories SET last_seen_tag = $1 WHERE name = $2`
	if _, err := d.pool.ExecContext(ctx, q, tag, repoName); err != nil {
		return fmt.Errorf("repository: update last_seen_tag: %w", err)
	}
	return nil
}

// CreateSubscription створює підписку зі статусом confirmed=false
func (d *DB) CreateSubscription(ctx context.Context, subscriberID, repositoryID int64, token string) error {
	const q = `
		INSERT INTO subscriptions (subscriber_id, repository_id, token)
		VALUES ($1, $2, $3)`

	if _, err := d.pool.ExecContext(ctx, q, subscriberID, repositoryID, token); err != nil {
		return fmt.Errorf("repository: create subscription: %w", err)
	}
	return nil
}

// FindSubscriptionByToken шукає підписку за токеном
func (d *DB) FindSubscriptionByToken(ctx context.Context, token string) (*models.Subscription, error) {
	var s models.Subscription
	const q = `SELECT id, subscriber_id, repository_id, token, confirmed FROM subscriptions WHERE token = $1`

	if err := d.pool.GetContext(ctx, &s, q, token); err != nil {
		return nil, fmt.Errorf("repository: find subscription by token: %w", err)
	}
	return &s, nil
}

// ConfirmSubscription підтверджує підписку за токеном
func (d *DB) ConfirmSubscription(ctx context.Context, token string) error {
	const q = `UPDATE subscriptions SET confirmed = TRUE WHERE token = $1`
	res, err := d.pool.ExecContext(ctx, q, token)
	if err != nil {
		return fmt.Errorf("repository: confirm subscription: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("repository: confirm subscription: token not found")
	}
	return nil
}

// DeleteSubscriptionByToken видаляє підписку (відписка користувача)
func (d *DB) DeleteSubscriptionByToken(ctx context.Context, token string) error {
	const q = `DELETE FROM subscriptions WHERE token = $1`
	res, err := d.pool.ExecContext(ctx, q, token)
	if err != nil {
		return fmt.Errorf("repository: delete subscription: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("repository: delete subscription: token not found")
	}
	return nil
}

// GetSubscriptionsByEmail повертає всі підписки для email (для GET /api/subscriptions)
func (d *DB) GetSubscriptionsByEmail(ctx context.Context, email string) ([]models.SubscriptionResponse, error) {
	const q = `
		SELECT
			sub.email,
			repo.name        AS repo,
			s.confirmed,
			repo.last_seen_tag
		FROM subscriptions s
			JOIN subscribers  sub  ON s.subscriber_id = sub.id
			JOIN repositories repo ON s.repository_id = repo.id
		WHERE sub.email = $1
		ORDER BY repo.name`

	rows := make([]models.SubscriptionResponse, 0)
	if err := d.pool.SelectContext(ctx, &rows, q, email); err != nil {
		return nil, fmt.Errorf("repository: get subscriptions by email: %w", err)
	}
	return rows, nil
}

// IsAlreadySubscribed перевіряє наявність підписки (будь-який статус confirmed)
func (d *DB) IsAlreadySubscribed(ctx context.Context, subscriberID, repositoryID int64) (bool, error) {
	var exists bool
	const q = `SELECT EXISTS(SELECT 1 FROM subscriptions WHERE subscriber_id=$1 AND repository_id=$2)`
	if err := d.pool.GetContext(ctx, &exists, q, subscriberID, repositoryID); err != nil {
		return false, fmt.Errorf("repository: is_already_subscribed: %w", err)
	}
	return exists, nil
}

// GetActiveSubscribersByRepo читає активних підписників з view для заданого репозиторію
func (d *DB) GetActiveSubscribersByRepo(ctx context.Context, repoName string) ([]models.ActiveSubscriber, error) {
	const q = `SELECT email, repo_name, last_seen_tag FROM active_subscribers_view WHERE repo_name = $1`

	subs := make([]models.ActiveSubscriber, 0)
	if err := d.pool.SelectContext(ctx, &subs, q, repoName); err != nil {
		return nil, fmt.Errorf("repository: get_active_subscribers: %w", err)
	}
	return subs, nil
}

// GetAllTrackedRepos повертає унікальні назви репозиторіїв з хоча б однією активною підпискою
func (d *DB) GetAllTrackedRepos(ctx context.Context) ([]string, error) {
	const q = `SELECT DISTINCT repo_name FROM active_subscribers_view ORDER BY repo_name`

	var repos []string
	if err := d.pool.SelectContext(ctx, &repos, q); err != nil {
		return nil, fmt.Errorf("repository: get_all_tracked_repos: %w", err)
	}
	return repos, nil
}
