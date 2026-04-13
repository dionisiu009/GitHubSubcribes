# GitHub Release Notification API

Монолітний сервіс для підписки на email-сповіщення про нові релізи репозиторіїв GitHub.

> [!NOTE]
> **Про коментарі в коді:** Детальні коментарі були додані в проект спеціально для етапу рецензування, щоб заощадити час перевіряючого на розбір логіки. У повсякденній практиці я дотримуюсь стандартів самодокументованого коду та мінімізую коментарі.

## Стек

| Компонент | Технологія |
|-----------|-----------|
| Мова | Go 1.21+ |
| HTTP | chi v5 |
| БД | PostgreSQL 16 + sqlx |
| Кеш | Redis 7 (TTL 10 хв) |
| Міграції | golang-migrate (авто при старті) |
| Email (dev) | Mailpit |
| Контейнери | Docker + docker-compose |

## Швидкий старт (локальна розробка)

### 1. Клонуй репозиторій та перейди до нього

```bash
git clone https://github.com/dionisiu009/GitHubSubcribes.git
cd GitHubSubcribes
```

### 2. Скопіюй та налаштуй змінні середовища

```bash
cp .env.example .env
```

Відкрий `.env` та встав свій GitHub Personal Access Token (необов'язково, але рекомендовано):

```env
GITHUB_KEY=ghp_ваш_токен
```

> Без токена — 60 запитів/год. З токеном — 5000 запитів/год.

### 3. Запусти всі сервіси через docker-compose

```bash
docker-compose -f docker-compose.dev.yml up --build
```

Docker підніме:
- **PostgreSQL** — `:5432`
- **Redis** — `:6379`
- **Mailpit** (SMTP + Web UI) — `:1025` / `:8025`
- **App** — `:8080`

Міграції БД застосовуються **автоматично** при старті застосунку.

### 4. Перевір що все працює

```bash
curl http://localhost:8080/health
# → {"status":"ok"}
```

### 5. Відкрий Mailpit UI

Всі вихідні листи перехоплюються Mailpit:

```
http://localhost:8025
```

---

## API Endpoints

### Підписатись на релізи

```bash
curl -X POST http://localhost:8080/api/subscribe \
  -H "Content-Type: application/json" \
  -d '{"email": "user@example.com", "repo": "golang/go"}'
```

### Підтвердити підписку

```bash
# Токен приходить у листі — або дивись у Mailpit
curl http://localhost:8080/api/confirm/{token}
```

### Відписатись

```bash
curl http://localhost:8080/api/unsubscribe/{token}
```

### Список підписок

```bash
curl "http://localhost:8080/api/subscriptions?email=user@example.com"
```

---

## Зупинка

```bash
docker-compose -f docker-compose.dev.yml down

# Знищити також volumes (БД та Redis)
docker-compose -f docker-compose.dev.yml down -v
```

---

## Архітектура

```
cmd/server/main.go          ← точка входу, DI, graceful shutdown
internal/
  config/config.go          ← читання змінних середовища
  api/
    handlers.go             ← HTTP-обробники (4 ендпоінти)
    router.go               ← chi маршрути + middleware
  service/subscription.go   ← бізнес-логіка (валідація, оркестрація)
  repository/postgres.go    ← SQL-запити, інтерфейс Repository, міграції
  scanner/
    github.go               ← GitHub API клієнт + Redis-кеш
    scanner.go              ← фоновий воркер (тікер 5 хв)
  notifier/email.go         ← відправка листів через net/smtp
  models/subscription.go    ← DB-моделі + DTO
migrations/
  000001_init_db.up.sql     ← таблиці + индекси + view
  000001_init_db.down.sql   ← відкат
pkg/
  logger/logger.go          ← slog-фабрика (text dev / JSON prod)
  redisclient/redis.go      ← go-redis клієнт з Ping
```

### Потік підписки

```
POST /subscribe
  → валідація email + repo
  → GitHub API (CheckRepoExists) — кешується 10 хв
  → upsert subscribers + repositories
  → перевірка дубліката (409 якщо є)
  → генерація UUID-токена
  → INSERT subscriptions (confirmed=false)
  → SendConfirmationEmail (Mailpit у dev)

GET /confirm/{token}
  → UPDATE confirmed=true

GET /unsubscribe/{token}
  → DELETE subscriptions

GET /subscriptions?email=
  → JOIN subscribers + subscriptions + repositories
```

### Алгоритм сканера

```
Тік кожні 5 хв:
  → SELECT DISTINCT repo_name FROM active_subscribers_view
  → для кожного репо: GetLatestRelease (GitHub API + кеш)
  → якщо latestTag != last_seen_tag:
      → GetActiveSubscribersByRepo (view)
      → SendReleaseNotification для кожного email
      → UpdateLastSeenTag
```

---

## Запуск тестів

```bash
go test ./... -v -race
```

---

## CI/CD

GitHub Actions автоматично запускає при кожному push:
1. `golangci-lint` — перевірка якості коду
2. `go test -race` — тести з реальними PostgreSQL та Redis