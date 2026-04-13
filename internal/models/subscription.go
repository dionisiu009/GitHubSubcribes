package models

import "time"

// Repository — запис про GitHub-репозиторій у БД
type Repository struct {
	ID          int64   `db:"id"`
	Name        string  `db:"name"`          // формат owner/repo
	LastSeenTag *string `db:"last_seen_tag"` // nil — тег ще не відомий
}

// Subscriber — підписник (унікальний email)
type Subscriber struct {
	ID        int64     `db:"id"`
	Email     string    `db:"email"`
	CreatedAt time.Time `db:"created_at"`
}

// Subscription — зв'язок підписника з репозиторієм
type Subscription struct {
	ID           int64  `db:"id"`
	SubscriberID int64  `db:"subscriber_id"`
	RepositoryID int64  `db:"repository_id"`
	Token        string `db:"token"` // UUID; використовується для підтвердження та відписки
	Confirmed    bool   `db:"confirmed"`
}

// ActiveSubscriber — рядок з active_subscribers_view
type ActiveSubscriber struct {
	Email       string  `db:"email"`
	RepoName    string  `db:"repo_name"`
	LastSeenTag *string `db:"last_seen_tag"`
}

// ---------- DTO для REST API ----------

// SubscribeRequest — тіло запиту POST /api/subscribe
type SubscribeRequest struct {
	Email string `json:"email"`
	Repo  string `json:"repo"` // формат owner/repo
}

// SubscriptionResponse — один елемент відповіді GET /api/subscriptions
type SubscriptionResponse struct {
	Email       string  `json:"email"`
	Repo        string  `json:"repo"`
	Confirmed   bool    `json:"confirmed"`
	LastSeenTag *string `json:"last_seen_tag,omitempty"`
}
