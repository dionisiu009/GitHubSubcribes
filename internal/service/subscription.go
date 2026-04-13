package service

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/google/uuid"

	"github.com/dionisiu009/GitHubSubcribes/internal/models"
	"github.com/dionisiu009/GitHubSubcribes/internal/notifier"
	"github.com/dionisiu009/GitHubSubcribes/internal/repository"
	"github.com/dionisiu009/GitHubSubcribes/internal/scanner"
)

// Сигнальні помилки — перевіряти через errors.Is()
var (
	// ErrAlreadySubscribed — підписка вже існує (HTTP 409)
	ErrAlreadySubscribed = errors.New("service: already subscribed")
	// ErrRepoNotFound — репозиторій не знайдено на GitHub (HTTP 404)
	ErrRepoNotFound = errors.New("service: repository not found")
	// ErrTokenNotFound — токен не існує у БД (HTTP 404)
	ErrTokenNotFound = errors.New("service: token not found")
	// ErrInvalidRepo — некоректний формат owner/repo (HTTP 400)
	ErrInvalidRepo = errors.New("service: invalid repository format")
	// ErrInvalidEmail — некоректний email (HTTP 400)
	ErrInvalidEmail = errors.New("service: invalid email")
)

// repoFormat — owner/repo: обидві частини непорожні, без пробілів
var repoFormat = regexp.MustCompile(`^[a-zA-Z0-9_.\-]+/[a-zA-Z0-9_.\-]+$`)

// emailFormat — спрощена перевірка наявності @ та домену
var emailFormat = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)

// SubscriptionService — бізнес-логіка підписок.
// Оркеструє: Repository ↔ GitHubClient ↔ Notifier.
type SubscriptionService struct {
	repo     repository.Repository
	github   *scanner.GitHubClient
	notifier notifier.Notifier
}

// New створює SubscriptionService з DI залежностей
func New(repo repository.Repository, gh *scanner.GitHubClient, n notifier.Notifier) *SubscriptionService {
	return &SubscriptionService{
		repo:     repo,
		github:   gh,
		notifier: n,
	}
}

// =============================================================================
// Публічні методи сервісу
// =============================================================================

// Subscribe оркеструє підписку:
// валідація → GitHub API → БД → відправка листа підтвердження
func (s *SubscriptionService) Subscribe(ctx context.Context, email, repoName string) error {
	// 1. Валідація вхідних даних
	if err := validateEmail(email); err != nil {
		return err
	}
	if err := validateRepo(repoName); err != nil {
		return err
	}

	// 2. Перевірка існування репозиторію через GitHub API (з кешем)
	if err := s.github.CheckRepoExists(ctx, repoName); err != nil {
		if errors.Is(err, scanner.ErrNotFound) {
			return ErrRepoNotFound
		}
		// ErrRateLimit або сітьова помилка — прокидаємо далі
		return fmt.Errorf("service: check repo: %w", err)
	}

	// 3. Upsert підписника та репозиторію у БД
	subscriber, err := s.repo.GetOrCreateSubscriber(ctx, email)
	if err != nil {
		return fmt.Errorf("service: get subscriber: %w", err)
	}

	repo, err := s.repo.GetOrCreateRepository(ctx, repoName)
	if err != nil {
		return fmt.Errorf("service: get repository: %w", err)
	}

	// 4. Перевірка дублікату підписки → 409
	exists, err := s.repo.IsAlreadySubscribed(ctx, subscriber.ID, repo.ID)
	if err != nil {
		return fmt.Errorf("service: check duplicate: %w", err)
	}
	if exists {
		return ErrAlreadySubscribed
	}

	// 5. Генеруємо унікальний UUID-токен для підтвердження та відписки
	token := uuid.New().String()

	// 6. Зберігаємо підписку (confirmed=false)
	if err = s.repo.CreateSubscription(ctx, subscriber.ID, repo.ID, token); err != nil {
		return fmt.Errorf("service: create subscription: %w", err)
	}

	// 7. Відправляємо лист підтвердження (помилка відправки не скасовує підписку)
	if err = s.notifier.SendConfirmationEmail(email, repoName, token); err != nil {
		// Логуємо але не повертаємо помилку клієнту — підписка вже є в БД,
		// адміністратор може відправити лист повторно
		return fmt.Errorf("service: send confirmation email: %w", err)
	}

	return nil
}

// Confirm підтверджує підписку за токеном
func (s *SubscriptionService) Confirm(ctx context.Context, token string) error {
	if err := validateToken(token); err != nil {
		return err
	}

	// Перевіряємо що токен існує — щоб розрізняти 400 (bad token) від 404 (not found)
	_, err := s.repo.FindSubscriptionByToken(ctx, token)
	if err != nil {
		return mapTokenError(err)
	}

	if err = s.repo.ConfirmSubscription(ctx, token); err != nil {
		return fmt.Errorf("service: confirm: %w", err)
	}
	return nil
}

// Unsubscribe видаляє підписку за токеном
func (s *SubscriptionService) Unsubscribe(ctx context.Context, token string) error {
	if err := validateToken(token); err != nil {
		return err
	}

	// Перевіряємо існування токена перед видаленням
	_, err := s.repo.FindSubscriptionByToken(ctx, token)
	if err != nil {
		return mapTokenError(err)
	}

	if err = s.repo.DeleteSubscriptionByToken(ctx, token); err != nil {
		return fmt.Errorf("service: unsubscribe: %w", err)
	}
	return nil
}

// GetSubscriptions повертає підписки для вказаного email
func (s *SubscriptionService) GetSubscriptions(ctx context.Context, email string) ([]models.SubscriptionResponse, error) {
	if err := validateEmail(email); err != nil {
		return nil, err
	}

	subs, err := s.repo.GetSubscriptionsByEmail(ctx, email)
	if err != nil {
		return nil, fmt.Errorf("service: get subscriptions: %w", err)
	}
	return subs, nil
}

// =============================================================================
// Допоміжні функції валідації
// =============================================================================

func validateEmail(email string) error {
	if strings.TrimSpace(email) == "" || !emailFormat.MatchString(email) {
		return ErrInvalidEmail
	}
	return nil
}

func validateRepo(repo string) error {
	if !repoFormat.MatchString(repo) {
		return ErrInvalidRepo
	}
	return nil
}

func validateToken(token string) error {
	// UUID v4 — 36 символів у форматі xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx
	if _, err := uuid.Parse(token); err != nil {
		return fmt.Errorf("%w: %s", ErrInvalidRepo, token)
	}
	return nil
}

// mapTokenError перетворює "not found" помилку репозиторію на сигнальну ErrTokenNotFound
func mapTokenError(err error) error {
	if err != nil && strings.Contains(err.Error(), "no rows") {
		return ErrTokenNotFound
	}
	return fmt.Errorf("service: lookup token: %w", err)
}
