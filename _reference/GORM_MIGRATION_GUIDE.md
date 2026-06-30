# Миграция на GORM + упразднение исторического функционала

> Инструкция для ИИ-агента (Claude Code / аналог), выполняющего рефакторинг.
> Читай документ целиком до начала работы. Выполняй пофайлово, по порядку из раздела 11.

---

## 0. Назначение и правила

**Цель.** Перевести слой доступа к БД с `sqlx` (поверх `github.com/lib/pq`) на **GORM** (`gorm.io/gorm` + `gorm.io/driver/postgres`), одновременно **упразднив функционал исторических таблиц и бэкапов**. Атомарную замену снимка дислокации сохранить по **варианту B** (быстрый swap через rename без истории).

**Жёсткие правила для агента:**

1. **Не меняй внешнее поведение API.** Сигнатуры публичных методов сервисов и хэндлеров остаются прежними, кроме явно удаляемых (раздел 5). HTTP-контракты, имена JSON-полей, коды ответов — без изменений.
2. **Пофайлово и атомарно.** Один репозиторий = один коммит. После каждого файла проект должен компилироваться (`go build ./...`) и проходить тесты.
3. **Сначала тест, потом рефакторинг.** Перед переписыванием файла напиши характеризующие тесты на его текущее поведение (раздел 10). Они — критерий «ничего не сломал».
4. **Не переписывай аналитический SQL в билдер GORM.** CTE, `DISTINCT ON`, агрегации, оконные функции остаются дословным SQL внутри `gorm.Raw().Scan()` (раздел 7, группа 3).
5. **Не используй `AutoMigrate` для таблиц со сложной схемой.** Структуру `disl_actual`/`disl_new` создавай через `CREATE TABLE ... LIKE ... INCLUDING ALL` (раздел 7, группа 4).
6. **Не угадывай имена столбцов.** Бери их из существующих `db:"..."` тегов моделей и из текста SQL-запросов.
7. **Перед удалением функции — найди все её вызовы** (`grep -rn ИмяМетода`). Удаляй снизу вверх: репозиторий → сервис-обёртка → хэндлер → роут. Если call-site уходит в код вне доступных файлов — остановись и зафиксируй это в отчёте, не оставляй «висячих» вызовов.
8. **Драйвер.** Целевой драйвер — pgx через `gorm.io/driver/postgres` (он использует pgx под капотом). Старый `github.com/lib/pq` удаляется из импортов по мере перевода файлов; финально — из `go.mod`.

**Стек на входе (факт):** `github.com/jmoiron/sqlx`, `github.com/lib/pq`. ~216 методов репозиториев, ~12 900 строк, паттерн «репозиторий поверх `*sqlx.DB`».

---

## 1. Целевая архитектура

- Подключение возвращает `*gorm.DB` вместо `*sqlx.DB`.
- Репозитории хранят `db *gorm.DB`.
- **Гибрид по умолчанию:** простой CRUD и динамические UPDATE — на билдере GORM; аналитика и оставшийся DDL — через `gorm.Raw()` / `gorm.Exec()`. Это штатный GORM, а не обход.
- Каждый репозиторий получает интерфейс (если ещё нет), чтобы вышестоящий код зависел от абстракции, а не от конкретного типа. Это позволяет мигрировать пофайлово.

---

## 2. Фаза 0 — подготовка (выполнить один раз, до файлов)

### 2.1 Зависимости

```bash
go get gorm.io/gorm
go get gorm.io/driver/postgres
```

`github.com/lib/pq` и `github.com/jmoiron/sqlx` пока **не удалять** — они нужны, пока не переведён последний файл.

### 2.2 Подключение (`db.go`)

Завести GORM поверх **того же** пула `database/sql`, чтобы sqlx и GORM работали над одним соединением и миграция шла пофайлово без даунтайма.

```go
// server/internal/db/db.go
package db

import (
	"context"
	"fmt"
	"log"
	"time"

	"gtport/server/internal/config"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Database на время миграции держит оба хэндла над одним пулом.
type Database struct {
	*sqlx.DB             // удалить после перевода последнего файла
	Gorm     *gorm.DB
}

func NewDB(ctx context.Context, cfg config.DBConfig) (*Database, error) {
	dsn := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		cfg.Host, cfg.Port, cfg.User, cfg.Password, cfg.Name, cfg.SSLMode)

	sqlxDB, err := sqlx.Connect("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database connection: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := sqlxDB.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	sqlxDB.SetMaxOpenConns(25)
	sqlxDB.SetMaxIdleConns(25)
	sqlxDB.SetConnMaxLifetime(5 * time.Minute)

	// GORM поверх того же *sql.DB.
	gormDB, err := gorm.Open(postgres.New(postgres.Config{
		Conn: sqlxDB.DB, // тот же пул
	}), &gorm.Config{
		Logger:                 logger.Default.LogMode(logger.Warn),
		SkipDefaultTransaction: true, // не оборачивать каждый Create/Update в неявную транзакцию
	})
	if err != nil {
		return nil, fmt.Errorf("failed to init gorm: %w", err)
	}

	log.Println("Successfully connected to database (sqlx + gorm)")
	return &Database{DB: sqlxDB, Gorm: gormDB}, nil
}
```

После Фазы 0 каждый репозиторий при переводе переключается с `db.DB` на `db.Gorm`. Когда переведён последний — из `Database` убирается встроенный `*sqlx.DB`, из `db.go` уходит `lib/pq` и `sqlx`.

### 2.3 Соглашение по моделям

GORM маппит по именам, но в проекте имена столбцов заданы тегами `db:"..."`. Чтобы ничего не поехало, **на каждую модель**:

- добавить метод `TableName() string`, возвращающий точное имя таблицы;
- задать столбцы тегом `gorm:"column:<имя>"`, перенося значения из существующих `db:"..."`;
- пометить первичный ключ `gorm:"primaryKey"`;
- столбцы, которые БД заполняет сама (`created_at`, `updated_at`, `RETURNING id`), пометить `gorm:"autoCreateTime"` / `gorm:"autoUpdateTime"` либо `default:...` где уместно.

Пример:

```go
type Client struct {
	ID           int       `gorm:"column:id;primaryKey" db:"id" json:"id"`
	Name         string    `gorm:"column:name"          db:"name" json:"name"`
	Code         string    `gorm:"column:code"          db:"code" json:"code"`
	ContactEmail string    `gorm:"column:contact_email" db:"contact_email" json:"contact_email"`
	ContactPhone string    `gorm:"column:contact_phone" db:"contact_phone" json:"contact_phone"`
	CreatedAt    time.Time `gorm:"column:created_at;autoCreateTime" db:"created_at" json:"created_at"`
	UpdatedAt    time.Time `gorm:"column:updated_at;autoUpdateTime" db:"updated_at" json:"updated_at"`
}

func (Client) TableName() string { return "clients" }
```

`db:"..."` теги пока **оставить** — они нужны файлам, ещё не переведённым на GORM.

---

## 3. Карта кода: четыре группы

Каждый метод репозитория относится к одной из групп. Стратегия зависит от группы.

| Группа | Что это | Файлы (основные) | Стратегия |
|---|---|---|---|
| 1. CRUD | Простые SELECT/INSERT/UPDATE/DELETE, m2m через JOIN | `client_`, `port_`, `token_`, `permission_`, `notification_`, `max_chat_`, `sf_`, `bros_reason_codes_`, бóльшая часть `user_` | Билдер GORM. Прямой выигрыш. |
| 2. Динамический UPDATE/WHERE | Ручная сборка `SET col=$n` со счётчиком плейсхолдеров | `history_` (≈стр. 333+), `vessel_call_`, `plan_`, `stock_` | `Model().Updates(map)` + `Where`. Главный выигрыш. |
| 3. Аналитика | CTE, `DISTINCT ON`, агрегации, `EXTRACT`, `LATERAL`, `UNION`, `STRING_AGG` | `bros_` (analytics), `history_` (CTE) | Оставить SQL дословно в `gorm.Raw().Scan()`. |
| 4. DDL / история | Динамические таблицы, swap, схема `history`, бэкапы, `information_schema` | `dislocation_db.go`, `history_tables_repository.go`, обёртки в `dislocation.go` | **Большей частью удаляется** (раздел 5). Остаётся swap по варианту B (раздел 7.4). |

---

## 4. Решение по группе 4 (Architecture Decision Record)

**Контекст.** Текущий код поддерживает: (а) загрузку нового снимка дислокации, (б) ротацию старых снимков в схему `history` с генерацией имён таблиц по timestamp, (в) ручные бэкапы и восстановление из них. Пункты (б) и (в) требуют динамического DDL, который несовместим с ORM-парадигмой и не даёт никакого выигрыша от GORM.

**Решение.** Упразднить (б) и (в). Сохранить только консистентную атомарную замену `disl_actual` свежим снимком из RAM по **варианту B**: данные грузятся в staging-таблицу `disl_new`, затем быстрый swap через `rename` подменяет `disl_actual`. Окно блокировки — доли секунды (только метаданные), история не ведётся.

**Последствие.** Удаляется весь динамический DDL, генерация имён, транслитерация, работа со схемой `history` и метаданными. Остаётся 3 строки DDL (`DROP` / `RENAME` / `CREATE ... LIKE`) внутри `gorm.Exec`.

**Требует подтверждения продукта.** Удаление просмотра истории и отката к бэкапу — продуктовое изменение. Если этот функционал используется в проде, согласовать удаление **до** начала работ. Агент: если найдёшь UI/эндпоинты, завязанные на бэкапы, — перечисли их в отчёте и жди подтверждения.

---

## 5. План упразднения функционала (группа 4)

Удаляй снизу вверх. Перед каждым удалением — `grep -rn` по имени, чтобы не оставить висячих вызовов.

### 5.1 Файл `history_tables_repository.go` — удалить целиком

Тип `HistoryTablesRepository` и все его методы (`CreateHistoryTable`, `GetHistoryTables`, `GetTableInfo`, метаданные `history_tables_metadata` и т.д.). Найти и удалить конструктор `NewHistoryTablesRepository` из места сборки зависимостей (DI/`main`/`router`).

### 5.2 Файл `dislocation_db.go` — удалить методы

Удалить:

- `SwapTablesInDB` (заменяется на `SwapStaging`, раздел 7.4);
- `sanitizeSourceType`, `normalizeSourceType`, `simplifiedTransliterate`, `transliterate` — нужны были только для имён исторических таблиц;
- `renameTableConstraints` — нужен был только при сосуществовании старой и новой таблиц;
- `cleanupOldHistoryTables`;
- `GetHistoryTables`, `GetHistoryTableData`, `GetHistoryTableInfo`;
- `BackupCurrentToHistory`, `backupCurrentToHistoryInternal`;
- `GetBackupTables`, `parseTimestampFromTableName`;
- `RestoreFromBackup`, `DeleteBackup`, `GetBackupInfo`.

Оставить (и перевести на GORM):

- `EnsureTablesExist` — упростить: гарантировать наличие `disl_actual` и `disl_new`, без схемы `history`;
- `SaveNewMapToDBSilent` → переименовать в `LoadStaging`, грузит в `disl_new` (раздел 7.4);
- `insertBatchToActual`/`insertBatch` → заменяется на `CreateInBatches`, ручные варианты удалить;
- `CheckTableExists` — оставить, если используется; перевести на GORM `Migrator().HasTable`.

### 5.3 Файл `dislocation.go` (слой Service) — удалить обёртки

Удалить методы `DislocationService`, проксирующие удалённое:

- `BackupCurrentToHistory` (≈стр. 420);
- `GetBackupTables` (≈стр. 425);
- `RestoreFromBackup` + `restoreFromBackupInternal` (≈стр. 430, 439);
- `DeleteBackup` (≈стр. 498);
- `GetBackupInfo` (≈стр. 503).

В `processDislocationInternal` (≈стр. 508) и в `processNewRecordsWithSource` / `processFromStage3WithSource` / `processFromStage4WithSource` — найти вызов `SwapTablesInDB`/`SaveNewMapToDB` и заменить связкой `LoadStaging` → `SwapStaging`. Параметры `sourceType`/`keepHistoryLimit`, которые шли только в историю, убрать из цепочки вызовов (но не ломать сигнатуры публичных методов сервиса без необходимости — если параметр приходит из хэндлера, оставь его принимаемым и игнорируй, либо вычисти по всей цепочке согласовав с хэндлером).

### 5.4 Хэндлеры и роуты

`grep -rn` по именам удалённых сервис-методов в каталоге хэндлеров/роутера (вне предоставленных файлов). Удалить соответствующие эндпоинты (бэкапы/история) и их регистрацию. Если фронтенд обращается к этим эндпоинтам — зафиксировать в отчёте список URL для отключения на клиенте.

---

## 6. Паттерны перевода: группа 1 (CRUD)

```go
// Get по id
err := r.db.First(&client, id).Error           // было: r.db.Get(&client, "... WHERE id=$1", id)

// Список с сортировкой
err := r.db.Order("name").Find(&clients).Error // было: r.db.Select(&clients, "... ORDER BY name")

// Insert с возвратом id/created_at — GORM сам читает их обратно в структуру
err := r.db.Create(client).Error               // RETURNING делается автоматически

// Update всех полей
err := r.db.Save(client).Error

// Delete
err := r.db.Delete(&models.Client{}, id).Error

// m2m через join: GetClientsByUserID
err := r.db.
	Joins("JOIN user_clients uc ON uc.client_id = clients.id").
	Where("uc.user_id = ?", userID).
	Find(&clients).Error
```

`sql.ErrNoRows` → у GORM это `gorm.ErrRecordNotFound`. Поправить проверки ошибок там, где они есть.

---

## 7. Паттерны перевода: группы 2–4

### 7.1 Группа 2 — динамический UPDATE

Заменить ручную сборку `setParts`/`paramCounter` на карту обновляемых полей:

```go
updates := map[string]any{}
if newIndexPp != nil  { updates["index_pp"]  = *newIndexPp }
if newDatePrib != nil { updates["date_prib"] = *newDatePrib }
// ... только непустые поля
updates["updated_at"] = time.Now()

err := r.db.Model(&models.Vagon{}).Where("id = ?", id).Updates(updates).Error
```

Если значение нужно выставить SQL-выражением (`NOW()`, `col + 1`), использовать `gorm.Expr`:

```go
updates["updated_at"] = gorm.Expr("NOW()")
```

### 7.2 Группа 3 — аналитика остаётся сырым SQL

SQL **не трогать**, поменять только обёртку:

```go
var rows []ReasonAnalyticsRow
err := r.db.Raw(query, startDate, endDate).Scan(&rows).Error
```

Целевые структуры результата (`ReasonAnalyticsRow` и пр.) маппятся по тегам — добавить им `gorm:"column:..."` рядом с `db:"..."`, имена столбцов брать из `AS`-алиасов в SQL.

### 7.3 Транзакции (группа 2, частично 1)

```go
err := r.db.Transaction(func(tx *gorm.DB) error {
	if err := tx.Create(user).Error; err != nil {
		return err
	}
	for _, p := range user.Ports {
		if err := tx.Exec(`INSERT INTO user_ports (user_id, port_id) VALUES (?, ?)`, user.ID, p.ID).Error; err != nil {
			return err
		}
	}
	return nil // возврат ошибки = rollback, nil = commit
})
```

### 7.4 Группа 4 — swap по варианту B (целевой код)

Полная замена оставшейся части `dislocation_db.go`. Модель `Dislocation` маппится на `disl_actual`; для staging используем `Table("disl_new")`.

```go
func (m *Dislocation) TableName() string { return "disl_actual" }

// EnsureTablesExist — гарантировать disl_actual и disl_new (без схемы history)
func (ds *DBService) EnsureTablesExist(ctx context.Context) error {
	db := ds.db.WithContext(ctx)
	if !db.Migrator().HasTable("disl_actual") {
		// disl_actual создаётся штатной схемной миграцией проекта (не AutoMigrate).
		return fmt.Errorf("base table disl_actual is missing; run schema migrations first")
	}
	if !db.Migrator().HasTable("disl_new") {
		if err := db.Exec(`CREATE TABLE disl_new (LIKE disl_actual INCLUDING ALL)`).Error; err != nil {
			return err
		}
	}
	return nil
}

// LoadStaging — залить новый снимок в disl_new (отдельная транзакция, чтобы окно swap было крошечным)
func (ds *DBService) LoadStaging(ctx context.Context, newMap map[string]models.Dislocation) error {
	records := make([]models.Dislocation, 0, len(newMap))
	for _, r := range newMap {
		records = append(records, r)
	}
	return ds.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`TRUNCATE TABLE disl_new`).Error; err != nil {
			return err
		}
		if len(records) == 0 {
			return nil
		}
		return tx.Table("disl_new").CreateInBatches(records, 500).Error
	})
}

// SwapStaging — атомарная замена disl_actual снимком из disl_new (вариант B)
func (ds *DBService) SwapStaging(ctx context.Context) error {
	return ds.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`DROP TABLE IF EXISTS disl_actual`).Error; err != nil {
			return err
		}
		if err := tx.Exec(`ALTER TABLE disl_new RENAME TO disl_actual`).Error; err != nil {
			return err
		}
		return tx.Exec(`CREATE TABLE disl_new (LIKE disl_actual INCLUDING ALL)`).Error
	})
}
```

**Свойства, которые обязаны сохраниться (проверить тестом):**

- DDL в PostgreSQL транзакционен → читатель `disl_actual` видит либо старый, либо новый снимок, без «пустого окна».
- Тяжёлая заливка — в `LoadStaging` (в `disl_new`, которую никто не читает); сам `SwapStaging` трогает только метаданные и потому быстрый.
- `CREATE TABLE ... LIKE ... INCLUDING ALL` оставлять как `Exec`, не заменять на `AutoMigrate` (иначе можно потерять индексы/дефолты, не описанные тегами).
- На модели `Dislocation` **не вешать GORM-хуки** (`BeforeCreate` и т.п.) — `CreateInBatches` вызовет их на каждой записи, для заливки снимка это лишнее.

В вызывающем коде (`dislocation.go`) последовательность `SaveNewMapToDB` + `SwapTablesInDB` заменяется на `LoadStaging(ctx, newMap)` затем `SwapStaging(ctx)`.

---

## 8. Известные подводные камни

1. **`ON CONFLICT ... COALESCE(...)`.** В проекте ~23 upsert; часть имеет в target конфликта выражение, например `ON CONFLICT (user_id, resource_type, COALESCE(resource_id, -1), action)`. `clause.OnConflict` в GORM **не умеет** выражения в target. Такие upsert оставлять как `gorm.Exec(сырой SQL)`. Простые upsert по колонкам можно перевести на `clause.OnConflict{Columns:..., DoUpdates:...}`.
2. **`RETURNING`.** `Create`/`Save` сами читают `RETURNING id, created_at, updated_at` в структуру — отдельный `QueryRowx(...).StructScan` не нужен. Проверить, что поля помечены корректными тегами.
3. **Кастомные сканеры / массивы.** В `cargowork_repository.go`, `history_repository.go`, `info_history_repository.go` есть `pq.Array`/кастомные `Scan/Value`. Для GORM реализовать `sql.Scanner` и `driver.Valuer` на соответствующих типах (разово на тип). `pq.StringArray` заменить на собственный тип или `pgtype`.
4. **Приведения типов `::date`, `::integer` и т.п.** (≈28 шт.) живут внутри сырого SQL групп 2–3 — их не трогаем, они остаются в `Raw`.
5. **`ErrNoRows` → `ErrRecordNotFound`.** Везде, где проверяется `errors.Is(err, sql.ErrNoRows)`, добавить/заменить на `errors.Is(err, gorm.ErrRecordNotFound)` для GORM-путей.
6. **`tile_db.go`.** Отдельное подключение (другой пул, `lib/pq`). Решить отдельно: либо так же завести GORM, либо оставить на sqlx (если это изолированный модуль тайлов). Зафиксировать выбор в отчёте.
7. **Контекст.** Везде использовать `db.WithContext(ctx)` — эквивалент `...Context` методов sqlx.

---

## 9. Соглашения по коду

- Репозиторий: поле `db *gorm.DB`, конструктор `NewXxxRepository(db *gorm.DB) *XxxRepository`.
- Имена методов и их сигнатуры — без изменений (кроме удаляемых).
- Ошибки оборачивать так же, как в оригинале (`fmt.Errorf("...: %w", err)`).
- Логи (`log.Printf("[Xxx] ...")`) сохранять — они используются для диагностики.

---

## 10. Тестирование

Перед переписыванием каждого файла:

1. Написать характеризующие тесты на текущее поведение его методов (вход → ожидаемый результат/побочный эффект в БД). Гонять на тестовой БД (testcontainers-postgres или локальная).
2. Перевести файл на GORM.
3. Те же тесты должны проходить без изменений в ассертах.

Отдельно для swap (раздел 7.4) — тест инварианта: во время `LoadStaging`+`SwapStaging` параллельный читатель `disl_actual` **никогда** не получает пустой результат и всегда видит согласованный набор (старый или новый), но не смесь.

---

## 11. Порядок выполнения (пофайлово)

Сначала простое — обкатать модели и подключение; динамику и удаление истории — в середине; сложное и завязанное на многое — в конце.

1. **Фаза 0** — `db.go` (раздел 2), зависимости, соглашение по моделям.
2. `client_repository.go` — эталон CRUD, на нём отладить теги моделей.
3. `port_repository.go`, `permission_repository.go`, `token_repository.go`, `max_chat_repository.go`, `sf_repository.go`, `bros_reason_codes_repository.go`, `notification_repository.go` — остальной CRUD.
4. `user_repository.go` — CRUD + транзакции + m2m + upsert (часть upsert оставить на `Exec`, см. п.8.1).
5. **Упразднение истории** (раздел 5): удалить `history_tables_repository.go`; вычистить методы из `dislocation_db.go` и обёртки из `dislocation.go`; удалить эндпоинты бэкапов/истории.
6. `dislocation_db.go` — перевести остаток на GORM: `EnsureTablesExist`, `LoadStaging`, `SwapStaging` (раздел 7.4); поправить вызовы в `dislocation.go`.
7. `vessel_call_repository.go`, `plan_repository.go`, `stock_repository.go`, `stock_repository_attis.go`, `port_report_repository.go`, `map_repository.go`, `rearrangement_repository.go`, `sms_plan_cache_repo.go` — CRUD + динамика.
8. `history_repository.go` — группы 2 и 3 (динамический UPDATE → `Updates(map)`; CTE/аналитику → `Raw`). Самый большой файл, делать последним из крупных.
9. `info_history_repository.go`, `cargowork_repository.go` — кастомные сканеры/массивы (п.8.3), затем CRUD/динамика.
10. `bros_repository.go` — CRUD + аналитика на `Raw` (CTE `latest_per_bros`).
11. `tile_repository.go` / `tile_db.go` — по решению из п.8.6.
12. **Финал:** убрать встроенный `*sqlx.DB` из `Database`; удалить из импортов и `go.mod` `github.com/jmoiron/sqlx` и `github.com/lib/pq`; `go mod tidy`; убрать `db:"..."` теги, если больше не нужны.

---

## 12. Definition of Done (по каждому файлу)

- [ ] Репозиторий использует `*gorm.DB`, импорты `sqlx`/`lib/pq` из файла удалены.
- [ ] Сигнатуры публичных методов не изменились (кроме явно удалённых).
- [ ] Аналитический SQL сохранён дословно в `Raw`.
- [ ] Характеризующие тесты зелёные.
- [ ] `go build ./...` и `go vet ./...` без ошибок.
- [ ] `grep -rn` не находит висячих вызовов удалённых методов.

## 13. Definition of Done (вся миграция)

- [ ] Все 11 пунктов раздела 11 выполнены.
- [ ] Исторический функционал и бэкапы удалены сверху донизу (репозиторий → сервис → хэндлер → роут → клиент уведомлён).
- [ ] Атомарная замена `disl_actual` работает по варианту B; инвариант «без пустого окна» подтверждён тестом.
- [ ] `go.mod` не содержит `sqlx` и `lib/pq` (кроме осознанно оставленного модуля тайлов, если так решено).
- [ ] Отчёт агента содержит: список удалённых эндпоинтов, решение по `tile_db.go`, список upsert, оставленных на сыром SQL, и любые места, где call-site уходил за пределы доступного кода.
