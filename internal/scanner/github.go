package scanner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/dionisiu009/GitHubSubcribes/internal/config"
)

// Сигнальні помилки — перевіряти через errors.Is()
var (
	// ErrRateLimit — вичерпано ліміт запитів GitHub API (HTTP 429 / X-RateLimit-Remaining: 0)
	ErrRateLimit = errors.New("github: rate limit exceeded")
	// ErrNotFound — репозиторій або реліз не знайдено (HTTP 404)
	ErrNotFound = errors.New("github: not found")
)

const (
	githubBaseURL = "https://api.github.com"
	// Таймаут одного HTTP-запиту до GitHub
	httpTimeout = 10 * time.Second
	// Префікс ключів у Redis
	cacheKeyPrefix = "gh:"
)

// GitHubRelease — мінімальна структура відповіді /releases/latest
type GitHubRelease struct {
	TagName string `json:"tag_name"` // наприклад "v1.2.3"
}

// GitHubClient — HTTP-клієнт до GitHub API з Redis-кешем
type GitHubClient struct {
	http  *http.Client
	redis *redis.Client
	cfg   config.GitHubConfig
	ttl   time.Duration // TTL кешу (з RedisConfig.CacheTTL)
}

// NewGitHubClient створює клієнт з налаштованим Bearer-токеном та Redis-кешем
func NewGitHubClient(ghCfg config.GitHubConfig, redisCfg config.RedisConfig, rdb *redis.Client) *GitHubClient {
	return &GitHubClient{
		http:  &http.Client{Timeout: httpTimeout},
		redis: rdb,
		cfg:   ghCfg,
		ttl:   redisCfg.CacheTTL,
	}
}

// CheckRepoExists перевіряє чи існує репозиторій на GitHub.
// Відповідь кешується у Redis; повертає ErrNotFound або ErrRateLimit при 404/429.
func (c *GitHubClient) CheckRepoExists(ctx context.Context, repo string) error {
	cacheKey := cacheKeyPrefix + "repo:" + repo

	// Перевіряємо кеш — якщо є, репо існує
	if cached, err := c.redis.Get(ctx, cacheKey).Result(); err == nil && cached == "1" {
		return nil
	}

	url := fmt.Sprintf("%s/repos/%s", githubBaseURL, repo)
	body, err := c.doRequest(ctx, url)
	if err != nil {
		return err // вже обгорнута ErrRateLimit або ErrNotFound
	}

	// Будь-яка непорожня відповідь означає що репо існує — кешуємо факт існування
	_ = body
	c.redis.Set(ctx, cacheKey, "1", c.ttl)
	return nil
}

// GetLatestRelease повертає тег останнього релізу репозиторію.
// Спочатку пробуємо /releases/latest; при 404 (немає релізів) пробуємо /tags.
// Результат кешується у Redis.
func (c *GitHubClient) GetLatestRelease(ctx context.Context, repo string) (string, error) {
	cacheKey := cacheKeyPrefix + "release:" + repo

	// Кеш hit
	if tag, err := c.redis.Get(ctx, cacheKey).Result(); err == nil {
		return tag, nil
	}

	// Спроба 1: /releases/latest
	tag, err := c.fetchLatestRelease(ctx, repo)
	if errors.Is(err, ErrNotFound) {
		// Репозиторій є, але публічних релізів нема — пробуємо через теги
		tag, err = c.fetchLatestTag(ctx, repo)
	}
	if err != nil {
		return "", err
	}
	if tag == "" {
		return "", ErrNotFound
	}

	c.redis.Set(ctx, cacheKey, tag, c.ttl)
	return tag, nil
}

// =============================================================================
// Приватні методи
// =============================================================================

// fetchLatestRelease звертається до /repos/{repo}/releases/latest
func (c *GitHubClient) fetchLatestRelease(ctx context.Context, repo string) (string, error) {
	url := fmt.Sprintf("%s/repos/%s/releases/latest", githubBaseURL, repo)
	body, err := c.doRequest(ctx, url)
	if err != nil {
		return "", err
	}

	var rel GitHubRelease
	if err = json.Unmarshal(body, &rel); err != nil {
		return "", fmt.Errorf("github: decode release: %w", err)
	}
	return rel.TagName, nil
}

// gitTag — мінімальна структура одного елемента з /tags
type gitTag struct {
	Name string `json:"name"`
}

// fetchLatestTag повертає перший тег з /repos/{repo}/tags (найновіший)
func (c *GitHubClient) fetchLatestTag(ctx context.Context, repo string) (string, error) {
	// per_page=1 — мінімум трафіку; GitHub повертає теги від найновішого
	url := fmt.Sprintf("%s/repos/%s/tags?per_page=1", githubBaseURL, repo)
	body, err := c.doRequest(ctx, url)
	if err != nil {
		return "", err
	}

	var tags []gitTag
	if err = json.Unmarshal(body, &tags); err != nil {
		return "", fmt.Errorf("github: decode tags: %w", err)
	}
	if len(tags) == 0 {
		return "", ErrNotFound
	}
	return tags[0].Name, nil
}

// doRequest виконує GET-запит до GitHub API.
// Додає заголовки авторизації та Accept; обробляє 404, 429 та інші помилки.
func (c *GitHubClient) doRequest(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("github: build request: %w", err)
	}

	// Версія API v3
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "gh-notify-service/1.0")

	// Автентифікуємось якщо є токен (5000 vs 60 req/hour)
	if c.cfg.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.cfg.Token)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("github: http request: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		// OK — читаємо тіло

	case http.StatusNotFound:
		return nil, ErrNotFound

	case http.StatusTooManyRequests, http.StatusForbidden:
		// 429 або 403 з вичерпаним лімітом
		return nil, c.buildRateLimitError(resp)

	default:
		return nil, fmt.Errorf("github: unexpected status %d for %s", resp.StatusCode, url)
	}

	// Обмежуємо читання тіла (JSON GitHub не перевищує кілька KB)
	buf := make([]byte, 0, 4096)
	tmp := make([]byte, 512)
	for {
		n, readErr := resp.Body.Read(tmp)
		buf = append(buf, tmp[:n]...)
		if readErr != nil {
			break
		}
	}
	return buf, nil
}

// buildRateLimitError формує помилку з деталями: скільки чекати та який ліміт
func (c *GitHubClient) buildRateLimitError(resp *http.Response) error {
	remaining := resp.Header.Get("X-RateLimit-Remaining")
	resetAt := resp.Header.Get("X-RateLimit-Reset") // Unix timestamp

	var resetIn string
	if ts, err := strconv.ParseInt(resetAt, 10, 64); err == nil {
		until := time.Until(time.Unix(ts, 0)).Truncate(time.Second)
		if until > 0 {
			resetIn = fmt.Sprintf(", скидання через %s", until)
		}
	}

	return fmt.Errorf("%w: remaining=%s%s", ErrRateLimit, remaining, resetIn)
}
