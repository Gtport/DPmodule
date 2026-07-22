// Package max — HTTP-адаптер исходящей рассылки в мессенджер MAX (перенос gtport
// internal/max/client.go). Реализует port.MessengerSender поверх Bot API MAX
// (https://platform-api.max.ru).
//
// Отличия от gtport, продиктованные архитектурой DPmodule:
//   - токен не хранится в структуре, а читается из SecretSource на каждый запрос
//     (env MAX_BOT_TOKEN сейчас, Vault потом) — как у ASU/reference-адаптеров;
//   - корневой сертификат Минцифры (Russian Trusted Root CA) вшит через go:embed
//     и добавлен в доверенный пул TLS ПОВЕРХ системного. platform-api.max.ru
//     отдаёт цепочку, подписанную нацвендором, — без этого якоря TLS не проходит.
//     Полагаться на системное хранилище ОС нельзя (сило: свой процесс — свой якорь).
//
// Отправка вложений (картинка/файл) в MAX трёхшаговая: получить URL загрузки →
// залить тело → отправить сообщение с токеном вложения. MAX обрабатывает файл
// асинхронно, поэтому после заливки — пауза и retry на attachment.not.ready
// (выстрадано боем в gtport, переносим как есть).
package max

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	"github.com/Gtport/DPmodule/internal/port"
)

// russianTrustedCA — корневой сертификат Минцифры (публичный, цепочка Russian
// Trusted Root CA + Sub CA). Вшит в бинарь: якорь TLS для platform-api.max.ru.
//
//go:embed certs/russian_trusted_ca.pem
var russianTrustedCA []byte

const (
	defaultBaseURL = "https://platform-api.max.ru"
	defaultTimeout = 120 * time.Second
	authHeader     = "Authorization" // MAX ждёт голый токен, без "Bearer"

	// Параметры дозагрузки вложений (как в gtport): MAX обрабатывает файл
	// асинхронно, сразу после заливки сообщение может отлететь not.ready.
	attachWait    = 3 * time.Second
	attachRetries = 5
	attachRetryIn = 2 * time.Second
)

// Client реализует port.MessengerSender.
type Client struct {
	baseURL       string
	authSecretKey string // ключ токена в SecretSource (дефолт MAX_BOT_TOKEN)
	secrets       port.SecretSource
	hc            *http.Client
}

// NewClient собирает клиент MAX. baseURL пуст → platform-api.max.ru; authSecretKey
// пуст → MAX_BOT_TOKEN; timeout ≤ 0 → 120s. TLS-пул = системный + вшитый CA Минцифры.
func NewClient(baseURL, authSecretKey string, timeout time.Duration, secrets port.SecretSource) (*Client, error) {
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	if authSecretKey == "" {
		authSecretKey = "MAX_BOT_TOKEN"
	}
	if timeout <= 0 {
		timeout = defaultTimeout
	}

	pool, err := x509.SystemCertPool()
	if err != nil || pool == nil {
		pool = x509.NewCertPool()
	}
	if !pool.AppendCertsFromPEM(russianTrustedCA) {
		return nil, fmt.Errorf("MAX: не удалось добавить корневой сертификат Минцифры в пул TLS")
	}

	hc := &http.Client{
		Timeout:   timeout,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool}},
	}
	return &Client{
		baseURL:       strings.TrimRight(baseURL, "/"),
		authSecretKey: authSecretKey,
		secrets:       secrets,
		hc:            hc,
	}, nil
}

// token читает токен бота из SecretSource на каждый запрос.
func (c *Client) token(ctx context.Context) (string, error) {
	t, err := c.secrets.Get(ctx, c.authSecretKey)
	if err != nil {
		return "", fmt.Errorf("MAX: секрет %q: %w", c.authSecretKey, err)
	}
	return t, nil
}

// Ping — GET /me: проверка доступности API и валидности токена (health-ручка).
func (c *Client) Ping(ctx context.Context) error {
	tok, err := c.token(ctx)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/me", nil)
	if err != nil {
		return fmt.Errorf("MAX Ping: сборка запроса: %w", err)
	}
	req.Header.Set(authHeader, tok)

	resp, err := c.hc.Do(req)
	if err != nil {
		return fmt.Errorf("MAX Ping: запрос: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("MAX Ping: статус %d: %s", resp.StatusCode, snippet(body))
	}
	return nil
}

// SendText отправляет текстовое сообщение в чат.
func (c *Client) SendText(ctx context.Context, chatID, text string) error {
	return c.sendMessage(ctx, chatID, messageRequest{Text: text})
}

// SendImage отправляет изображение с подписью (type=image).
func (c *Client) SendImage(ctx context.Context, chatID string, image []byte, filename, caption string) error {
	if len(image) == 0 {
		return fmt.Errorf("MAX SendImage: пустое изображение")
	}
	return c.sendAttachment(ctx, chatID, image, filename, caption, "image")
}

// SendFile отправляет произвольный файл с подписью (type=file).
func (c *Client) SendFile(ctx context.Context, chatID string, file []byte, filename, caption string) error {
	if len(file) == 0 {
		return fmt.Errorf("MAX SendFile: пустой файл")
	}
	return c.sendAttachment(ctx, chatID, file, filename, caption, "file")
}

// ── Вложения: URL загрузки → заливка → сообщение с retry ─────────────────────

// sendAttachment реализует общую трёхшаговую отправку вложения. kind — тип
// загрузки MAX: "image" (токен приходит вложенным в photos.{id}.token) либо
// "file" (токен приходит напрямую в поле token).
func (c *Client) sendAttachment(ctx context.Context, chatID string, data []byte, filename, caption, kind string) error {
	tok, err := c.token(ctx)
	if err != nil {
		return err
	}

	uploadURL, err := c.getUploadURL(ctx, tok, kind)
	if err != nil {
		return fmt.Errorf("MAX %s: URL загрузки: %w", kind, err)
	}
	attachTok, err := c.upload(ctx, tok, uploadURL, data, filename, kind)
	if err != nil {
		return fmt.Errorf("MAX %s: заливка: %w", kind, err)
	}

	// MAX обрабатывает вложение асинхронно — ждём, затем шлём с retry.
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(attachWait):
	}

	payload, _ := json.Marshal(map[string]string{"token": attachTok})
	msg := messageRequest{
		Text:        caption,
		Attachments: []attachment{{Type: kind, Payload: payload}},
	}

	var lastErr error
	for attempt := 1; attempt <= attachRetries; attempt++ {
		lastErr = c.sendMessageTok(ctx, tok, chatID, msg)
		if lastErr == nil {
			return nil
		}
		// Только «файл ещё не готов» имеет смысл повторять.
		if strings.Contains(lastErr.Error(), "attachment.not.ready") ||
			strings.Contains(lastErr.Error(), "not.processed") {
			if attempt < attachRetries {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(attachRetryIn):
				}
				continue
			}
		}
		return lastErr
	}
	return fmt.Errorf("MAX %s: не обработано после %d попыток: %w", kind, attachRetries, lastErr)
}

// getUploadURL — шаг 1: POST /uploads?type=<kind> → URL для заливки тела.
func (c *Client) getUploadURL(ctx context.Context, tok, kind string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/uploads?type="+kind, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set(authHeader, tok)

	resp, err := c.hc.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("статус %d: %s", resp.StatusCode, snippet(body))
	}
	var out struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", fmt.Errorf("разбор ответа: %w", err)
	}
	if out.URL == "" {
		return "", fmt.Errorf("пустой URL загрузки: %s", snippet(body))
	}
	return out.URL, nil
}

// upload — шаг 2: multipart-заливка тела на uploadURL, возврат токена вложения.
// Для image токен лежит в photos.{id}.token, для file — в token напрямую.
func (c *Client) upload(ctx context.Context, tok, uploadURL string, data []byte, filename, kind string) (string, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := w.CreateFormFile("data", filename)
	if err != nil {
		return "", err
	}
	if _, err := part.Write(data); err != nil {
		return "", err
	}
	if err := w.Close(); err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, uploadURL, &buf)
	if err != nil {
		return "", err
	}
	req.Header.Set(authHeader, tok)
	req.Header.Set("Content-Type", w.FormDataContentType())

	resp, err := c.hc.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("статус %d: %s", resp.StatusCode, snippet(body))
	}

	if kind == "image" {
		var out struct {
			Photos map[string]struct {
				Token string `json:"token"`
			} `json:"photos"`
		}
		if err := json.Unmarshal(body, &out); err != nil {
			return "", fmt.Errorf("разбор ответа: %w", err)
		}
		for _, p := range out.Photos {
			if p.Token != "" {
				return p.Token, nil
			}
		}
		return "", fmt.Errorf("нет токена изображения: %s", snippet(body))
	}

	var out struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", fmt.Errorf("разбор ответа: %w", err)
	}
	if out.Token == "" {
		return "", fmt.Errorf("нет токена файла: %s", snippet(body))
	}
	return out.Token, nil
}

// ── Отправка сообщения ───────────────────────────────────────────────────────

type messageRequest struct {
	Text        string       `json:"text,omitempty"`
	Attachments []attachment `json:"attachments,omitempty"`
}

type attachment struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// sendMessage читает токен и шлёт сообщение (для текстовых отправок).
func (c *Client) sendMessage(ctx context.Context, chatID string, msg messageRequest) error {
	tok, err := c.token(ctx)
	if err != nil {
		return err
	}
	return c.sendMessageTok(ctx, tok, chatID, msg)
}

// sendMessageTok — POST /messages?chat_id=<id> с уже полученным токеном (чтобы
// не перечитывать секрет между retry вложения).
func (c *Client) sendMessageTok(ctx context.Context, tok, chatID string, msg messageRequest) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/messages?chat_id="+chatID, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set(authHeader, tok)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.hc.Do(req)
	if err != nil {
		return fmt.Errorf("запрос: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("MAX сообщение: статус %d: %s", resp.StatusCode, snippet(body))
	}
	return nil
}

// snippet — короткий безопасный фрагмент тела ответа для текста ошибки.
func snippet(b []byte) string {
	const max = 200
	s := strings.TrimSpace(string(b))
	cut := len(s) > max
	if cut {
		s = s[:max]
	}
	s = strings.ToValidUTF8(s, "")
	if cut {
		s += "…"
	}
	return s
}
