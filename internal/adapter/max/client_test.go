package max

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// staticSecret — SecretSource с фиксированным токеном для тестов.
type staticSecret struct {
	token string
	err   error
}

func (s staticSecret) Get(context.Context, string) (string, error) {
	return s.token, s.err
}

// newTestClient направляет клиент на httptest-сервер (минуя реальный TLS/CA).
func newTestClient(t *testing.T, baseURL, token string) *Client {
	t.Helper()
	c, err := NewClient(baseURL, "", 5*time.Second, staticSecret{token: token})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return c
}

func TestEmbeddedCAParses(t *testing.T) {
	// Вшитый сертификат Минцифры обязан складываться в пул — иначе TLS к MAX
	// не поднимется, и это должно падать здесь, а не в бою.
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(russianTrustedCA) {
		t.Fatal("вшитый russian_trusted_ca.pem не разобран в пул сертификатов")
	}
}

func TestPing(t *testing.T) {
	var gotAuth, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"user_id":1,"name":"bot"}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, "TOK")
	if err := c.Ping(context.Background()); err != nil {
		t.Fatalf("Ping: %v", err)
	}
	if gotPath != "/me" {
		t.Errorf("путь Ping = %q, ждали /me", gotPath)
	}
	if gotAuth != "TOK" {
		t.Errorf("Authorization = %q, ждали голый токен TOK (без Bearer)", gotAuth)
	}
}

func TestPingBadStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"code":"unauthorized"}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, "TOK")
	if err := c.Ping(context.Background()); err == nil {
		t.Fatal("ждали ошибку при статусе 401")
	}
}

func TestSendText(t *testing.T) {
	var gotChat, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotChat = r.URL.Query().Get("chat_id")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, "TOK")
	if err := c.SendText(context.Background(), "-123", "привет"); err != nil {
		t.Fatalf("SendText: %v", err)
	}
	if gotChat != "-123" {
		t.Errorf("chat_id = %q, ждали -123", gotChat)
	}
	var msg messageRequest
	if err := json.Unmarshal([]byte(gotBody), &msg); err != nil {
		t.Fatalf("тело не JSON: %v (%s)", err, gotBody)
	}
	if msg.Text != "привет" {
		t.Errorf("text = %q, ждали привет", msg.Text)
	}
	if len(msg.Attachments) != 0 {
		t.Errorf("у текстового сообщения не должно быть вложений")
	}
}

// TestSendImage проверяет всю трёхшаговую цепочку заливки: /uploads → PUT тела →
// /messages с токеном вложения из photos.{id}.token.
func TestSendImage(t *testing.T) {
	var steps []string
	var sentPayload string
	mux := http.NewServeMux()
	// Шаг 3: сообщение с вложением.
	mux.HandleFunc("/messages", func(w http.ResponseWriter, r *http.Request) {
		steps = append(steps, "message")
		var msg messageRequest
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &msg)
		if len(msg.Attachments) == 1 {
			sentPayload = string(msg.Attachments[0].Payload)
		}
		w.WriteHeader(http.StatusOK)
	})
	// Шаг 1: URL загрузки. Указываем на /upload того же сервера.
	mux.HandleFunc("/uploads", func(w http.ResponseWriter, r *http.Request) {
		steps = append(steps, "uploads:"+r.URL.Query().Get("type"))
		_, _ = w.Write([]byte(`{"url":"` + baseOf(r) + `/upload"}`))
	})
	// Шаг 2: заливка тела, отдаём токен изображения.
	mux.HandleFunc("/upload", func(w http.ResponseWriter, r *http.Request) {
		steps = append(steps, "upload")
		_, _ = w.Write([]byte(`{"photos":{"p1":{"token":"IMGTOK"}}}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := newTestClient(t, srv.URL, "TOK")
	// attachWait/retry реально ждут — уменьшать не станем, тест короткий (3с пауза).
	if err := c.SendImage(context.Background(), "-9", []byte("PNGDATA"), "plan.png", "форма"); err != nil {
		t.Fatalf("SendImage: %v", err)
	}
	want := "uploads:image"
	if len(steps) != 3 || steps[0] != want || steps[1] != "upload" || steps[2] != "message" {
		t.Fatalf("порядок шагов = %v, ждали [%s upload message]", steps, want)
	}
	if !strings.Contains(sentPayload, "IMGTOK") {
		t.Errorf("payload вложения = %q, ждали токен IMGTOK", sentPayload)
	}
}

func TestSendImageEmpty(t *testing.T) {
	c := newTestClient(t, "http://unused", "TOK")
	if err := c.SendImage(context.Background(), "-9", nil, "x.png", ""); err == nil {
		t.Fatal("ждали ошибку на пустом изображении")
	}
}

// baseOf восстанавливает http://host для ответа /uploads.
func baseOf(r *http.Request) string {
	return "http://" + r.Host
}
