// Package lkrobot — клиент личного кабинета РЖД (cargolk.rzd.ru) для
// автовыгрузки дислокации вместо ручной работы диспетчера.
//
// Кабинет — обычное Rails-приложение с JSON-API, браузер не нужен. Порядок тот
// же, что проходит человек по экранам (Информация → Дислокация → новый запрос →
// роль «грузополучатель» → запросить → отметить все → скачать Excel):
//
//	GET  /sign_in                                → cookie + csrf-токен из <meta>
//	POST /sign_in                                → вход {user:{query,password}}
//	POST /api/v1/services/asoup/reports          → заказ отчёта по ОКПО
//	GET  /api/v1/services/asoup/reports/{id}     → этим кабинет рисует таблицу:
//	                                               здесь уже все строки (head/body)
//	PUT  /api/v1/services/asoup/reports/{id}.xlsx → файл по отмеченным вагонам
//
// Строки из шага 4 нужны и сами по себе (номера вагонов = «отметить все»), и как
// признак готовности отчёта. Состав колонок файла — встроенный шаблон
// report_columns.json, снятый с живого кабинета.
package lkrobot

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"regexp"
	"time"
)

// Шаблон колонок отчёта: тот же набор из 67 колонок, что диспетчер получал
// руками (кабинет отдаёт файл ровно по этому описанию).
//
//go:embed report_columns.json
var reportColumnsJSON []byte

var (
	// ErrAuth — кабинет не принял логин/пароль. Отделён от прочих: диспетчеру
	// надо показать «неверный пароль», а не «кабинет недоступен».
	ErrAuth = errors.New("ЛК РЖД: вход не выполнен (логин или пароль)")
	// ErrNotReady — отчёт не подготовился за отведённое время.
	ErrNotReady = errors.New("ЛК РЖД: отчёт не подготовился за отведённое время")
	// ErrEmpty — кабинет вернул отчёт без строк (нечего отдавать в приём).
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
		Data   struct {
			Head []string            `json:"head"`
			Body [][]json.RawMessage `json:"body"`
		} `json:"data"`
	} `json:"data"`
}

// Fetch — полный цикл по одному ОКПО: заказ отчёта, ожидание готовности,
// выгрузка xlsx. Возвращает содержимое файла — ровно то, что диспетчер скачивал
// руками, и его же принимает LKIntake.
func (c *Client) Fetch(ctx context.Context, okpo string) ([]byte, int, error) {
	id, err := c.order(ctx, okpo)
	if err != nil {
		return nil, 0, err
	}
	cars, err := c.waitCars(ctx, id)
	if err != nil {
		return nil, 0, err
	}
	file, err := c.download(ctx, id, cars)
	if err != nil {
		return nil, 0, err
	}
	return file, len(cars), nil
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

// waitCars ждёт готовности отчёта и возвращает номера вагонов из таблицы —
// это же список для выгрузки (эквивалент галочки «выбрать все» на экране).
func (c *Client) waitCars(ctx context.Context, id int64) ([]string, error) {
	deadline := time.Now().Add(c.opt.PollTimeout)
	path := fmt.Sprintf("/api/v1/services/asoup/reports/%d?id=%d&minimal=true", id, id)
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(c.opt.PollEvery):
		}

		resp, err := c.do(ctx, http.MethodGet, path, nil, true)
		if err != nil {
			return nil, fmt.Errorf("ЛК РЖД: опрос отчёта: %w", err)
		}
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			var out reportResponse
			if err := json.Unmarshal(raw, &out); err != nil {
				return nil, fmt.Errorf("ЛК РЖД: опрос отчёта: разбор ответа: %w", err)
			}
			if out.Data.Status == "done" {
				return carNumbers(out.Data.Data.Head, out.Data.Data.Body)
			}
			if out.Data.Status == "error" || out.Data.Status == "failed" {
				return nil, fmt.Errorf("ЛК РЖД: кабинет вернул ошибку по отчёту (%s)", out.Data.Status)
			}
		}
		if time.Now().After(deadline) {
			return nil, ErrNotReady
		}
	}
}

// carNumbers достаёт колонку NOM_VAG. Значения приходят строками, но кабинет
// иногда отдаёт число — поэтому разбираем сырой JSON.
func carNumbers(head []string, body [][]json.RawMessage) ([]string, error) {
	idx := -1
	for i, h := range head {
		if h == "NOM_VAG" {
			idx = i
			break
		}
	}
	if idx < 0 {
		return nil, errors.New("ЛК РЖД: в ответе нет колонки NOM_VAG")
	}
	cars := make([]string, 0, len(body))
	for _, row := range body {
		if idx >= len(row) {
			continue
		}
		var s string
		if err := json.Unmarshal(row[idx], &s); err != nil {
			var n json.Number
			if err2 := json.Unmarshal(row[idx], &n); err2 != nil {
				continue
			}
			s = n.String()
		}
		if s != "" {
			cars = append(cars, s)
		}
	}
	if len(cars) == 0 {
		return nil, ErrEmpty
	}
	return cars, nil
}

func (c *Client) download(ctx context.Context, id int64, cars []string) ([]byte, error) {
	var tpl struct {
		CustomFields json.RawMessage `json:"custom_fields"`
	}
	if err := json.Unmarshal(reportColumnsJSON, &tpl); err != nil {
		return nil, fmt.Errorf("ЛК РЖД: шаблон колонок: %w", err)
	}
	body, _ := json.Marshal(map[string]any{"custom_fields": tpl.CustomFields, "objects": cars})

	resp, err := c.do(ctx, http.MethodPut, fmt.Sprintf("/api/v1/services/asoup/reports/%d.xlsx", id), body, true)
	if err != nil {
		return nil, fmt.Errorf("ЛК РЖД: выгрузка файла: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ЛК РЖД: выгрузка файла: ответ %d", resp.StatusCode)
	}
	file, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("ЛК РЖД: выгрузка файла: %w", err)
	}
	// xlsx — zip; проверяем сигнатуру, чтобы не отдать в приём html-страницу ошибки.
	if len(file) < 2 || file[0] != 'P' || file[1] != 'K' {
		return nil, fmt.Errorf("ЛК РЖД: выгрузка файла: пришёл не xlsx (%d байт)", len(file))
	}
	return file, nil
}
