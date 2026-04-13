package notifier

import (
	"bytes"
	"fmt"
	"html/template"
	"net/smtp"
	"strings"
	"time"

	"github.com/dionisiu009/GitHubSubcribes/internal/config"
)

// Notifier — інтерфейс відправника листів (зручно для моків у тестах)
type Notifier interface {
	SendConfirmationEmail(to, repoName, token string) error
	SendReleaseNotification(to, repoName, newTag string) error
}

// EmailNotifier — реалізація через net/smtp (Mailpit у dev, реальний SMTP у prod)
type EmailNotifier struct {
	cfg     config.SMTPConfig
	baseURL string // для побудови посилань підтвердження / відписки
}

// New створює EmailNotifier
func New(smtpCfg config.SMTPConfig, baseURL string) *EmailNotifier {
	return &EmailNotifier{cfg: smtpCfg, baseURL: baseURL}
}

// SendConfirmationEmail відправляє лист з посиланням підтвердження підписки
func (n *EmailNotifier) SendConfirmationEmail(to, repoName, token string) error {
	data := struct {
		RepoName    string
		ConfirmURL  string
		UnsubURL    string
		CurrentYear int
	}{
		RepoName:    repoName,
		ConfirmURL:  fmt.Sprintf("%s/api/confirm/%s", n.baseURL, token),
		UnsubURL:    fmt.Sprintf("%s/api/unsubscribe/%s", n.baseURL, token),
		CurrentYear: time.Now().Year(),
	}

	subject := fmt.Sprintf("Підтвердіть підписку на релізи %s", repoName)
	body, err := renderTemplate(confirmationTmpl, data)
	if err != nil {
		return fmt.Errorf("notifier: render confirmation: %w", err)
	}

	return n.send(to, subject, body)
}

// SendReleaseNotification відправляє лист про новий реліз
func (n *EmailNotifier) SendReleaseNotification(to, repoName, newTag string) error {
	data := struct {
		RepoName    string
		NewTag      string
		ReleaseURL  string
		UnsubURL    string
		CurrentYear int
	}{
		RepoName:    repoName,
		NewTag:      newTag,
		ReleaseURL:  fmt.Sprintf("https://github.com/%s/releases/tag/%s", repoName, newTag),
		UnsubURL:    fmt.Sprintf("%s/api/unsubscribe/{{TOKEN}}", n.baseURL), // токен підставляє сканер
		CurrentYear: time.Now().Year(),
	}

	subject := fmt.Sprintf("🚀 Новий реліз %s: %s", repoName, newTag)
	body, err := renderTemplate(releaseTmpl, data)
	if err != nil {
		return fmt.Errorf("notifier: render release: %w", err)
	}

	return n.send(to, subject, body)
}

// SendReleaseNotificationWithToken — варіант із конкретним токеном відписки (для сканера)
func (n *EmailNotifier) SendReleaseNotificationWithToken(to, repoName, newTag, unsubToken string) error {
	data := struct {
		RepoName    string
		NewTag      string
		ReleaseURL  string
		UnsubURL    string
		CurrentYear int
	}{
		RepoName:    repoName,
		NewTag:      newTag,
		ReleaseURL:  fmt.Sprintf("https://github.com/%s/releases/tag/%s", repoName, newTag),
		UnsubURL:    fmt.Sprintf("%s/api/unsubscribe/%s", n.baseURL, unsubToken),
		CurrentYear: time.Now().Year(),
	}

	subject := fmt.Sprintf("🚀 Новий реліз %s: %s", repoName, newTag)
	body, err := renderTemplate(releaseTmpl, data)
	if err != nil {
		return fmt.Errorf("notifier: render release: %w", err)
	}

	return n.send(to, subject, body)
}

// =============================================================================
// Внутрішня відправка
// =============================================================================

// send формує MIME-повідомлення та відправляє через SMTP
func (n *EmailNotifier) send(to, subject, htmlBody string) error {
	addr := fmt.Sprintf("%s:%d", n.cfg.Host, n.cfg.Port)

	// MIME-заголовки + HTML тіло
	msg := buildMIME(n.cfg.From, to, subject, htmlBody)

	var auth smtp.Auth
	// Mailpit не потребує авторизації; для реальних SMTP (SendGrid, Gmail) — PlainAuth
	if n.cfg.User != "" && n.cfg.Password != "" {
		auth = smtp.PlainAuth("", n.cfg.User, n.cfg.Password, n.cfg.Host)
	}

	if err := smtp.SendMail(addr, auth, n.cfg.From, []string{to}, []byte(msg)); err != nil {
		return fmt.Errorf("notifier: send mail to %s: %w", to, err)
	}
	return nil
}

// buildMIME збирає рядок повідомлення у форматі RFC 2822 з HTML-альтернативою
func buildMIME(from, to, subject, htmlBody string) string {
	var sb strings.Builder
	sb.WriteString("MIME-Version: 1.0\r\n")
	sb.WriteString(fmt.Sprintf("From: %s\r\n", from))
	sb.WriteString(fmt.Sprintf("To: %s\r\n", to))
	sb.WriteString(fmt.Sprintf("Subject: %s\r\n", subject))
	sb.WriteString("Content-Type: text/html; charset=\"utf-8\"\r\n")
	sb.WriteString("\r\n")
	sb.WriteString(htmlBody)
	return sb.String()
}

// renderTemplate рендерить HTML-шаблон з підстановкою даних
func renderTemplate(tmplStr string, data any) (string, error) {
	t, err := template.New("email").Parse(tmplStr)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err = t.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// =============================================================================
// HTML-шаблони листів
// =============================================================================

const confirmationTmpl = `<!DOCTYPE html>
<html lang="uk">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>Підтвердження підписки</title>
  <style>
    body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif;
           background: #f6f8fa; margin: 0; padding: 32px 16px; color: #24292f; }
    .card { background: #ffffff; border-radius: 12px; max-width: 520px;
            margin: 0 auto; padding: 40px 36px;
            box-shadow: 0 1px 3px rgba(0,0,0,.12); }
    .logo { font-size: 28px; margin-bottom: 4px; }
    h2 { margin: 12px 0 8px; font-size: 20px; }
    p  { color: #57606a; line-height: 1.6; margin: 0 0 20px; }
    .repo { display: inline-block; background: #ddf4ff; color: #0550ae;
            border-radius: 6px; padding: 2px 10px; font-family: monospace;
            font-size: 15px; margin-bottom: 24px; }
    .btn { display: inline-block; background: #1f883d; color: #ffffff !important;
           text-decoration: none; padding: 12px 28px; border-radius: 8px;
           font-size: 15px; font-weight: 600; margin-bottom: 28px; }
    .footer { font-size: 12px; color: #8c959f; border-top: 1px solid #d0d7de;
              padding-top: 20px; margin-top: 8px; }
    .unsub  { color: #8c959f; font-size: 12px; }
  </style>
</head>
<body>
  <div class="card">
    <div class="logo">🔔</div>
    <h2>Підтвердьте підписку на GitHub-релізи</h2>
    <p>Ви запросили сповіщення про нові релізи репозиторію:</p>
    <div class="repo">{{.RepoName}}</div>
    <p>Натисніть кнопку нижче, щоб підтвердити підписку:</p>
    <a class="btn" href="{{.ConfirmURL}}">✅ Підтвердити підписку</a>
    <p>Якщо ви не надсилали цей запит — просто ігноруйте лист.</p>
    <div class="footer">
      <p class="unsub">Не хочете отримувати листи?
        <a href="{{.UnsubURL}}">Відписатись</a>
      </p>
      <p>© {{.CurrentYear}} GitHub Release Notifier</p>
    </div>
  </div>
</body>
</html>`

const releaseTmpl = `<!DOCTYPE html>
<html lang="uk">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>Новий реліз {{.RepoName}}</title>
  <style>
    body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif;
           background: #f6f8fa; margin: 0; padding: 32px 16px; color: #24292f; }
    .card { background: #ffffff; border-radius: 12px; max-width: 520px;
            margin: 0 auto; padding: 40px 36px;
            box-shadow: 0 1px 3px rgba(0,0,0,.12); }
    .badge { font-size: 32px; margin-bottom: 8px; }
    h2 { margin: 12px 0 8px; font-size: 20px; }
    p  { color: #57606a; line-height: 1.6; margin: 0 0 16px; }
    .repo { display: inline-block; background: #ddf4ff; color: #0550ae;
            border-radius: 6px; padding: 2px 10px; font-family: monospace;
            font-size: 15px; }
    .tag  { display: inline-block; background: #dafbe1; color: #116329;
            border-radius: 20px; padding: 4px 14px; font-family: monospace;
            font-size: 18px; font-weight: 700; margin: 16px 0 24px; }
    .btn  { display: inline-block; background: #0969da; color: #ffffff !important;
            text-decoration: none; padding: 12px 28px; border-radius: 8px;
            font-size: 15px; font-weight: 600; margin-bottom: 28px; }
    .footer { font-size: 12px; color: #8c959f; border-top: 1px solid #d0d7de;
              padding-top: 20px; margin-top: 8px; }
    .unsub { color: #8c959f; font-size: 12px; }
  </style>
</head>
<body>
  <div class="card">
    <div class="badge">🚀</div>
    <h2>Новий реліз вийшов!</h2>
    <p>Репозиторій <span class="repo">{{.RepoName}}</span> опублікував нову версію:</p>
    <div class="tag">{{.NewTag}}</div>
    <p>Перегляньте що нового у цьому релізі:</p>
    <a class="btn" href="{{.ReleaseURL}}">Переглянути реліз на GitHub</a>
    <div class="footer">
      <p class="unsub">Більше не хочете отримувати сповіщення?
        <a href="{{.UnsubURL}}">Відписатись</a>
      </p>
      <p>© {{.CurrentYear}} GitHub Release Notifier</p>
    </div>
  </div>
</body>
</html>`
