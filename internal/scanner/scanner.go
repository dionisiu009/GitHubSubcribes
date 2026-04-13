package scanner

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/dionisiu009/GitHubSubcribes/internal/notifier"
	"github.com/dionisiu009/GitHubSubcribes/internal/repository"
)

// Scanner — фоновий воркер: перевіряє нові релізи та розсилає сповіщення
type Scanner struct {
	repo     repository.Repository
	github   *GitHubClient
	notifier notifier.Notifier
	interval time.Duration
	log      *slog.Logger
}

// New створює Scanner
func NewScanner(
	repo repository.Repository,
	gh *GitHubClient,
	n notifier.Notifier,
	interval time.Duration,
	log *slog.Logger,
) *Scanner {
	return &Scanner{
		repo:     repo,
		github:   gh,
		notifier: n,
		interval: interval,
		log:      log,
	}
}

// Run запускає нескінченний цикл сканування; зупиняється при закритті ctx
func (s *Scanner) Run(ctx context.Context) {
	s.log.Info("сканер запущено", "interval", s.interval)

	// Перша ітерація одразу при старті, не чекаємо перший тік
	s.scan(ctx)

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			s.log.Info("сканер зупинено")
			return
		case <-ticker.C:
			s.scan(ctx)
		}
	}
}

// scan — одна ітерація: обходить усі репозиторії та перевіряє нові релізи
func (s *Scanner) scan(ctx context.Context) {
	s.log.Debug("починаємо сканування репозиторіїв")

	// Отримуємо всі підтверджені підписки через view
	// Для цього достатньо отримати унікальні репо — просимо перший-ліпший email
	// щоб дістати список репо (view поверне їх усі)
	repos, err := s.fetchUniqueRepos(ctx)
	if err != nil {
		s.log.Error("не вдалося отримати список репозиторіїв", "err", err)
		return
	}

	s.log.Debug("знайдено репозиторіїв", "count", len(repos))

	for _, repoName := range repos {
		if ctx.Err() != nil {
			return // graceful shutdown під час ітерації
		}
		s.checkRepo(ctx, repoName)
	}
}

// checkRepo перевіряє один репозиторій на новий реліз
func (s *Scanner) checkRepo(ctx context.Context, repoName string) {
	// 1. Отримуємо останній тег з GitHub (кешовано у Redis)
	latestTag, err := s.github.GetLatestRelease(ctx, repoName)
	if err != nil {
		if errors.Is(err, ErrRateLimit) {
			s.log.Warn("rate limit GitHub API — зупиняємо поточну ітерацію", "repo", repoName, "err", err)
			return
		}
		if errors.Is(err, ErrNotFound) {
			s.log.Debug("реліз не знайдено, пропускаємо", "repo", repoName)
			return
		}
		s.log.Error("помилка отримання релізу", "repo", repoName, "err", err)
		return
	}

	// 2. Беремо підписників цього репо з view (contains last_seen_tag)
	subscribers, err := s.repo.GetActiveSubscribersByRepo(ctx, repoName)
	if err != nil {
		s.log.Error("помилка отримання підписників", "repo", repoName, "err", err)
		return
	}
	if len(subscribers) == 0 {
		return
	}

	// 3. Порівнюємо тег: last_seen_tag зберігається в таблиці repositories,
	//    до view він потрапляє через JOIN — беремо з першого рядка
	currentTag := ""
	if subscribers[0].LastSeenTag != nil {
		currentTag = *subscribers[0].LastSeenTag
	}

	if latestTag == currentTag {
		s.log.Debug("новий реліз відсутній", "repo", repoName, "tag", latestTag)
		return
	}

	s.log.Info("новий реліз знайдено!", "repo", repoName, "old", currentTag, "new", latestTag)

	// 4. Розсилаємо листи всім підписникам
	sentCount := 0
	for _, sub := range subscribers {
		if err = s.notifier.SendReleaseNotification(sub.Email, repoName, latestTag); err != nil {
			s.log.Error("помилка відправки листа", "email", sub.Email, "repo", repoName, "err", err)
			continue
		}
		sentCount++
	}

	s.log.Info("листи відправлено", "repo", repoName, "sent", sentCount, "total", len(subscribers))

	// 5. Оновлюємо last_seen_tag лише після успішної розсилки
	//    (навіть якщо частина листів не дійшла — уникаємо нескінченної розсилки)
	if err = s.repo.UpdateLastSeenTag(ctx, repoName, latestTag); err != nil {
		s.log.Error("не вдалося оновити last_seen_tag", "repo", repoName, "err", err)
	}
}

// fetchUniqueRepos повертає список унікальних репозиторіїв з активними підписками.
// Використовує active_subscribers_view: запитуємо всі рядки без фільтру по repo.
func (s *Scanner) fetchUniqueRepos(ctx context.Context) ([]string, error) {
	// Хак: передаємо порожній рядок → view поверне всі рядки;
	// але GetActiveSubscribersByRepo фільтрує по repo_name — потрібен окремий метод.
	// Делегуємо до репозиторію.
	return s.repo.GetAllTrackedRepos(ctx)
}
