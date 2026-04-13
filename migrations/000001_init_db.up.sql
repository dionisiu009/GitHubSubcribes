-- Репозиторії GitHub, за якими стежимо
CREATE TABLE IF NOT EXISTS repositories (
    id            SERIAL PRIMARY KEY,
    name          VARCHAR(255) UNIQUE NOT NULL,   -- формат owner/repo
    last_seen_tag VARCHAR(255)                    -- останній відомий тег релізу
);

-- Унікальні підписники (один email — один запис)
CREATE TABLE IF NOT EXISTS subscribers (
    id         SERIAL PRIMARY KEY,
    email      VARCHAR(255) UNIQUE NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Зв'язок підписника з репозиторієм
CREATE TABLE IF NOT EXISTS subscriptions (
    id            SERIAL PRIMARY KEY,
    subscriber_id INTEGER NOT NULL REFERENCES subscribers(id) ON DELETE CASCADE,
    repository_id INTEGER NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    token         VARCHAR(255) UNIQUE NOT NULL,  -- UUID для підтвердження / відписки
    confirmed     BOOLEAN NOT NULL DEFAULT FALSE,

    -- один підписник не може двічі підписатись на той самий репо
    CONSTRAINT uq_sub_repo UNIQUE (subscriber_id, repository_id)
);

-- Індекси для швидкого пошуку по токену та email
CREATE INDEX IF NOT EXISTS idx_subscriptions_token        ON subscriptions(token);
CREATE INDEX IF NOT EXISTS idx_subscriptions_confirmed    ON subscriptions(confirmed);
CREATE INDEX IF NOT EXISTS idx_subscribers_email          ON subscribers(email);

-- View для сканера: лише активні (підтверджені) підписки
CREATE VIEW active_subscribers_view AS
SELECT
    sub.email,
    repo.name           AS repo_name,
    repo.last_seen_tag
FROM subscriptions s
    JOIN subscribers  sub  ON s.subscriber_id = sub.id
    JOIN repositories repo ON s.repository_id = repo.id
WHERE s.confirmed = TRUE;
