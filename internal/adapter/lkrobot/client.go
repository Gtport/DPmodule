// Package lkrobot — клиент личного кабинета РЖД (cargolk.rzd.ru) для
// автовыгрузки дислокации вместо ручной работы диспетчера.
//
// Кабинет — обычное Rails-приложение с JSON-API, браузер не нужен. Порядок тот
// же, что проходит человек по экранам (Информация → Дислокация → новый запрос →
// роль «грузополучатель» → запросить), но без последнего шага «скачать Excel»:
//
//	GET  /sign_in                            → cookie + csrf-токен из <meta>
//	POST /sign_in                            → вход {user:{query,password}}
//	POST /api/v1/services/asoup/reports      → заказ отчёта по ОКПО
//	GET  /api/v1/services/asoup/reports/{id} → готовая таблица: head + body + created_at
//
// Файл не качаем (решение владельца 03.08.2026): ответ последнего шага — тот, чем
// кабинет рисует таблицу на экране, — уже несёт все строки и 136 полей АСОУП, из
// которых дислокации нужно ~30. Сверка с xlsx того же среза: 28 из 32 полей 1:1,
// оставшиеся расходятся в пользу JSON (секунды в датах, станция отправления там,
// где Excel отдавал пустую ячейку). Ручной приём xlsx работает как прежде.
package lkrobot

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"go.uber.org/zap"
)

var (
	// ErrAuth — кабинет не принял логин/пароль. Отделён от прочих: диспетчеру
	// надо показать «неверный пароль», а не «кабинет недоступен».
	ErrAuth = errors.New("ЛК РЖД: вход не выполнен (логин или пароль)")
	// ErrNotReady — отчёт не подготовился за отведённое время.
	ErrNotReady = errors.New("ЛК РЖД: отчёт не подготовился за отведённое время")
	// ErrEmpty — кабинет вернул отчёт без строк (обновлять снимок нечем).
	ErrEmpty = errors.New("ЛК РЖД: отчёт пуст")
)

var csrfRe = regexp.MustCompile(`<meta name="csrf-token" content="([^"]+)"`)

// Options — настройки клиента (из секции lk_robot конфига).
type Options struct {
	BaseURL     string
	ServiceID   int
	Timeout     time.Duration
	PollEvery   time.Duration
	PollTimeout time.Duration
	// Log — журнал адаптера: пишет состав таблицы (имена полей и число строк)
	// при каждом заборе, чтобы смена контракта кабинета была заметна сразу, а не
	// по кривым данным в снимке. Может быть nil (тесты).
	Log *zap.Logger
	// DumpDir — папка для сырого ответа отчёта. Пусто — не сохранять. Нужна,
	// чтобы разбирать контракт кабинета на настоящих данных (форматы значений
	// видны только глазами) и снимать с них golden-тесты. Дамп перезаписывается
	// по ОКПО: интересен последний, а не история.
	DumpDir string
}

// Client — одна сессия кабинета. Не переиспользуется между запусками: сессия
// ЛК короткая и не переживает даже перезапуск, поэтому вход делается каждый раз.
type Client struct {
	opt  Options
	http *http.Client
	csrf string
}

func New(opt Options) (*Client, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	return &Client{
		opt:  opt,
		http: &http.Client{Jar: jar, Timeout: opt.Timeout},
	}, nil
}

// userAgent — кабинет отдаёт вёрстку и API одинаково всем, но пустой UA у
// прокси РЖД иногда режется; представляемся обычным браузером.
const userAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/151.0 Safari/537.36"

func (c *Client) do(ctx context.Context, method, path string, body []byte, json bool) (*http.Response, error) {
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.opt.BaseURL+path, rdr)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	if c.csrf != "" {
		req.Header.Set("X-CSRF-Token", c.csrf)
	}
	if json {
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/plain, */*")
	}
	return c.http.Do(req)
}

// grabCSRF читает токен из <meta name="csrf-token"> страницы. Токен обязателен
// для всех изменяющих запросов и меняется после входа.
func (c *Client) grabCSRF(ctx context.Context, path string) error {
	resp, err := c.do(ctx, http.MethodGet, path, nil, false)
	if err != nil {
		return fmt.Errorf("ЛК РЖД: %s: %w", path, err)
	}
	defer resp.Body.Close()
	html, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("ЛК РЖД: %s: %w", path, err)
	}
	if m := csrfRe.FindSubmatch(html); m != nil {
		c.csrf = string(m[1])
		return nil
	}
	return fmt.Errorf("ЛК РЖД: %s: не найден csrf-токен", path)
}

// Login — вход в кабинет. sms_code кабинет принимает пустым: второго фактора у
// используемых учёток нет.
func (c *Client) Login(ctx context.Context, login, password string) error {
	if err := c.grabCSRF(ctx, "/sign_in"); err != nil {
		return err
	}
	body, _ := json.Marshal(map[string]any{
		"user": map[string]any{"query": login, "password": password, "sms_code": nil},
	})
	resp, err := c.do(ctx, http.MethodPost, "/sign_in", body, true)
	if err != nil {
		return fmt.Errorf("ЛК РЖД: вход: %w", err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	switch {
	case resp.StatusCode == http.StatusCreated || resp.StatusCode == http.StatusOK:
	case resp.StatusCode == http.StatusUnauthorized, resp.StatusCode == http.StatusForbidden,
		resp.StatusCode == http.StatusUnprocessableEntity, resp.StatusCode == http.StatusBadRequest:
		return ErrAuth
	default:
		return fmt.Errorf("ЛК РЖД: вход: неожиданный ответ %d", resp.StatusCode)
	}
	// После входа токен другой — перечитываем с главной, иначе API ответит 422.
	return c.grabCSRF(ctx, "/")
}

// orderResponse — ответ на заказ отчёта: номер приходит либо в конверте data,
// либо на верхнем уровне (кабинет отвечает по-разному на разных услугах).
type orderResponse struct {
	ID   int64 `json:"id"`
	Data struct {
		ID int64 `json:"id"`
	} `json:"data"`
}

type reportResponse struct {
	Data struct {
		Status string `json:"status"`
		// CreatedAt — метка формирования среза («03.08.2026 02:21», МСК). Это то же
		// значение, что приём вычитывал из шапки xlsx (сверено 03.08.2026), и на нём
		// стоят гарды свежести и doc_ts журнала.
		CreatedAt string `json:"created_at"`
		Data      struct {
			Head []string            `json:"head"`
			Body [][]json.RawMessage `json:"body"`
		} `json:"data"`
	} `json:"data"`
}

// Table — готовый отчёт кабинета: имена полей АСОУП, строки значений и метка
// формирования среза (created_at, московское время как отдал кабинет). Ровно те
// же данные, что попадали в xlsx, только без промежуточного файла.
type Table struct {
	Head      []string
	Body      [][]json.RawMessage
	CreatedAt string
}

// FetchTable — полный цикл по одному ОКПО: заказ отчёта, ожидание готовности,
// разбор таблицы. Файл НЕ скачивается: ответ опроса уже несёт все строки и все
// поля, нужные дислокации (проверено сверкой с xlsx того же среза 03.08.2026 —
// 28 из 32 полей 1:1, остальные расходятся в пользу JSON). Ручной приём файлов
// это не отменяет — там xlsx приносит человек.
func (c *Client) FetchTable(ctx context.Context, okpo string) (Table, error) {
	id, err := c.order(ctx, okpo)
	if err != nil {
		return Table{}, err
	}
	return c.waitTable(ctx, id, okpo)
}

func (c *Client) order(ctx context.Context, okpo string) (int64, error) {
	// Фильтр — тот же, что шлёт экран кабинета при роли «грузополучатель».
	body, _ := json.Marshal(map[string]any{"params": map[string]any{
		"group": []any{}, "cars": []any{}, "country": map[string]any{}, "by_els": false,
		"invoices": []any{}, "freights": []any{}, "destination_stations": []any{},
		"load_stations": []any{}, "senders": []any{},
		"receivers":  []string{okpo},
		"foreign":    map[string]any{"id": "01", "text": "По РФ"},
		"date":       "",
		"sort":       map[string]any{"id": "no_sort", "text": "Без сортировки"},
		"sort_field": "NOM_VAG",
		"service_id": c.opt.ServiceID,
	}})
	resp, err := c.do(ctx, http.MethodPost, "/api/v1/services/asoup/reports", body, true)
	if err != nil {
		return 0, fmt.Errorf("ЛК РЖД: заказ отчёта: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode == http.StatusUnauthorized {
		return 0, ErrAuth
	}
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("ЛК РЖД: заказ отчёта: ответ %d", resp.StatusCode)
	}
	var out orderResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return 0, fmt.Errorf("ЛК РЖД: заказ отчёта: разбор ответа: %w", err)
	}
	id := out.Data.ID
	if id == 0 {
		id = out.ID
	}
	if id == 0 {
		return 0, errors.New("ЛК РЖД: заказ отчёта: в ответе нет номера")
	}
	return id, nil
}

// waitTable ждёт готовности отчёта и возвращает таблицу целиком. Тем же запросом
// кабинет рисует таблицу на экране, поэтому в ответе уже все строки и все поля.
func (c *Client) waitTable(ctx context.Context, id int64, okpo string) (Table, error) {
	deadline := time.Now().Add(c.opt.PollTimeout)
	path := fmt.Sprintf("/api/v1/services/asoup/reports/%d?id=%d&minimal=true", id, id)
	for {
		select {
		case <-ctx.Done():
			return Table{}, ctx.Err()
		case <-time.After(c.opt.PollEvery):
		}

		resp, err := c.do(ctx, http.MethodGet, path, nil, true)
		if err != nil {
			return Table{}, fmt.Errorf("ЛК РЖД: опрос отчёта: %w", err)
		}
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			var out reportResponse
			if err := json.Unmarshal(raw, &out); err != nil {
				return Table{}, fmt.Errorf("ЛК РЖД: опрос отчёта: разбор ответа: %w", err)
			}
			if out.Data.Status == "done" {
				c.logColumns(id, out.Data.Data.Head, len(out.Data.Data.Body))
				c.dump(okpo, raw)
				if len(out.Data.Data.Body) == 0 {
					return Table{}, ErrEmpty
				}
				return Table{
					Head:      out.Data.Data.Head,
					Body:      out.Data.Data.Body,
					CreatedAt: out.Data.CreatedAt,
				}, nil
			}
			if out.Data.Status == "error" || out.Data.Status == "failed" {
				return Table{}, fmt.Errorf("ЛК РЖД: кабинет вернул ошибку по отчёту (%s)", out.Data.Status)
			}
		}
		if time.Now().After(deadline) {
			return Table{}, ErrNotReady
		}
	}
}

// logColumns записывает состав готовой таблицы: имена колонок (коды АСОУП,
// вроде NOM_VAG/DATE_OP) и число строк. Данные вагонов не пишем — только
// заголовки, чтобы видеть, чем кабинет отвечает на `minimal=true`, и замечать,
// если состав однажды поменяется. Строка одна на кабинет за запуск.
func (c *Client) logColumns(id int64, head []string, rows int) {
	if c.opt.Log == nil {
		return
	}
	c.opt.Log.Info("ЛК РЖД: состав таблицы отчёта",
		zap.Int64("report_id", id),
		zap.Int("columns", len(head)),
		zap.Strings("head", head),
		zap.Int("rows", rows))
}

// dump сохраняет сырой ответ отчёта в DumpDir (файл на ОКПО, перезаписывается).
// Ошибка записи забор не валит — это диагностика, а не часть работы.
func (c *Client) dump(okpo string, raw []byte) {
	if c.opt.DumpDir == "" {
		return
	}
	if err := os.MkdirAll(c.opt.DumpDir, 0o755); err == nil {
		err = os.WriteFile(filepath.Join(c.opt.DumpDir, "report_"+okpo+".json"), raw, 0o644)
		if err == nil {
			return
		}
	}
	if c.opt.Log != nil {
		c.opt.Log.Warn("ЛК РЖД: сырой ответ не сохранён", zap.String("okpo", okpo), zap.String("dir", c.opt.DumpDir))
	}
}
