# HANDOFF — состояние проекта DPmodule (передача на новую машину)

> Документ-передача для **нового Claude Code**, продолжающего работу после переезда.
> Самодостаточен: читается вместе с `CLAUDE.md`, `TARGET.md`, `PROJECT_INSTRUCTIONS.md`.
> Дата среза: **2026-07-10**. Последняя работа — подсистема «план подвода»
> (P-1a парсер, P-2a матч, P-2b запись в снимок, P-1b хранение сетки) **+ фронтенд**
> загрузки ЛК и плана подвода (PR #43, влито в `main`).
> P-2b/P-1b и фронтенд **проверены на живом проде** (`app.gtport.ru`): реальные файлы
> ЛК (НМТП+АТТИС) загружены и обработаны в снимок через UI, план перезалит для `ma`/`nk`.

---

## 0. КРИТИЧНО — что НЕ в git и потеряется с папкой (сохранить вручную!)

Код в безопасности: `DPmodule` (`git@github.com:Gtport/DPmodule.git`) и эталон
`gtlogic` (`git@github.com:Gtport/gtlogic.git`) — на GitHub, переклонируются. НО вне
git живёт следующее — **скопировать до сдачи машины**:

| Что | Путь | Почему важно |
|---|---|---|
| Секреты/креды БД | `.env` (`PG_PASSWORD` и пр.) | не восстановить; нужен для запуска. Уже заведён в `DPmodule/.env` (права 600) + подключён в systemd-юнит `dpmodule-backend` через `EnvironmentFile=` (см. §8) |
| Seed справочников | `_reference/seed/*.csv` | stations(3124, 403 БАМ)/ports/marka/route_speed/naznach_station/cargo_operations — **источник правды для пересева БД** |
| Локальный конфиг | `config.yaml` | боевые значения (DSN, storage); в git — шаблон |
| Тест-фикстуры | `/home/alex/projects/new_go/*.xlsx`, `*.json` | планы подвода (`Мыс Астафьева.xlsx`=ma, `Находка.xlsx`=nk), JSON-выгрузки (nmtp/attis), ЛК |
| Dev-Keycloak | `deploy/keycloak/` | realm с тестовыми юзерами |
| **Память проекта** | `~/.claude/projects/-home-alex-projects/memory/` | накопленный контекст (см. §7); НЕ уедет с репом |
| **Транскрипты сессий** | `~/.claude/projects/-home-alex-projects/*.jsonl` | история решений |
| БД dpport | PostgreSQL на VPS | если рискует и VPS — `pg_dump dpport > dump.sql` |

Если БД `dpport` уцелеет — seed можно переэкспортировать из неё; если нет — CSV в
`_reference/seed/` и есть первичный источник. **`_data/`** (загруженные файлы) —
некритично, восстановимо повторной загрузкой.

---

## 1. Что за проект (одной страницей)

Переписываем **GTport** (инструмент диспетчера логоцентра: учёт дислокации вагонов в
морпортах) из жёстко зашитого под 3 порта (АЭ/УТ-1/ГУТ-2) в **универсальный**
настраиваемый прототип. Способ — **greenfield**: пишем заново на архитектуре шаблона
`TMPL_backend` (Ports & Adapters, DI в `main.go`), **переносим бизнес-логику** из
`gtlogic` (= старый GTport, референс, **не трогать**). Порт-специфику — в БД/реестры,
не в код.

- **Владелец** — логист, не программист. Отвечать по-русски, технически точно, объяснять
  что/где/почему/как проверить.
- **Эталон GTport — истина.** Новый код даёт те же результаты на тех же входах.
  Отклонения — только с явного согласия владельца (зафиксированы ниже).
- Полные правила — в `CLAUDE.md` (жёсткие правила) и `PROJECT_INSTRUCTIONS.md`.

## 2. Работа вдвоём (важно для git)

Два клона на одном VPS под логином `alex`, синхронизация **только через GitHub**:
- `/home/alex/projects/DPmodule` — клон **владельца**;
- `/home/alex/projects/DPmodule-lead` — клон **тимлида** (здесь работает ассистент).

Правила: **один клон = один автор** (в чужой не лезть); **в `main` не коммитить** —
ветка `feat/<задача>` → push → PR → владелец смотрит diff и мёржит; **перед стартом
`git pull`**, ветку от свежего `main`; `_reference/seed/` синхронизируется вручную
(вне git). Свой PR **не мёржить** — это делает владелец.

## 3. Архитектура и жёсткие правила (нарушать нельзя без просьбы)

- **Слои:** `domain` (чистые типы) → `port` (интерфейсы) → `service` (логика, RAM-кэши) →
  `repository/gorm` (БД, ORM-модель отдельно от доменной) → `handler` (gin) →
  провод в `internal/server/server.go` + `cmd/server/main.go`.
- **Время — Московское naive, без TZ.** Тип `domain.LocalTime` (без `Z`). «Сейчас» —
  только `clock.Now()` (единственное место с `LoadLocation`). **Запрещено:**
  `LoadLocation`/`FixedZone`/`.In`/`.UTC()`/сдвиги `Add(±7h)`. Правило «час≥18 →
  +сутки» — бизнес-правило (не TZ), остаётся.
- **Никакого нового хардкода портов** — порт-данные в таблицы + реестр, не switch/map.
- **GORM-гибрид:** билдер для CRUD/динамических UPDATE; сырой SQL дословно для
  аналитики/DDL/атомарной подмены. Снапшот-таблицам — без хуков. Схему ведёт
  `golang-migrate` (`cmd/migrate`), **не** AutoMigrate. Миграции идемпотентные,
  только добавляющие.
- **Снимок дислокации — «вариант Б»:** запись только через `ReplaceActual` (заливка в
  staging → атомарный swap). Мутации RAM на месте НЕТ — `ActualCache` перечитывается
  через `Load` после swap.
- **Падать громко** на целостности данных. Минимальные изменения, не переписывать модуль.
- ⚠️ **CRLF-файлы — НЕ прогонять через gofmt:** `internal/server/server.go`,
  `internal/config/config.go`, `cmd/migrate/main.go` (и др. из шаблона) хранят CRLF.
  gofmt перебьёт CRLF→LF и создаст гигантский ложный diff. Править точечно
  (Edit целевой строки; при вставке — сохранять `\r\n`).
- Перед коммитом **показывать diff, ждать «коммить»/«да»**. `config.yaml`, `ROADMAP.md`,
  `_reference/seed/` — **не коммитить** никогда.

## 4. Что готово (движок дислокации)

Влито в `main` (детали — в памяти `dislocation-pipeline-progress`, см. §7):

- **Приём ЛК** (Excel) + **JSON-ingest задел** (`ProcessRecords`, `cmd/jsonrun`). Боевой
  прогон 5000 записей ✅ (nmtp 4420 + attis 580, резолв в порты, marka 4857, история 5000).
- **Stage 1** (`Enricher.Stage1`): станции → идентификация порта+фильтр → операции →
  статусы (0/1/2/4/5/6/9/10/12) → производные. Парсер-агностичен (LK и JSON → один конвейер).
- **Stage 2**: S2-0 `ActualCache` (RAM-снимок) · S2-1 таблица `status9` (кандидаты
  8-пропал/9-живой) · S2-2 carry-over (`enrichFromActual`) · S2-3 marka+назначение+
  донорство перегруза (`status6`) · S2-5 расчёт хода (`forecast.go`, route_speed) ·
  S2-6 запись `vagon_history`.
- **RAM-кэши** всего состояния: `DirectoryCache`/`ActualCache`/`Status9Cache`/`Status6Cache`
  (БД только прогрев на старте + запись).
- **Справочники**: stations/cargo_operations/marka/ports/route_speed/naznach_station +
  сид. `ports` имеет `plan_code`('ma'/'nk'/'rb') и `NameS` (краткое имя причала).

**Ещё НЕ сделано (после плана):** Stage 4 (ProgMsk/ProgJd прогноз по плану) · S2-4
(очереди bros/простои, статусы 4 и 5 — **разные таблицы**, аналог bros; статус 4 в
gtlogic no-op — наша новая функциональность) · трейл `vagon_operation` · полноценный
JSON-ingest по HTTP · read-API + фронт для просмотра самой дислокации (загрузка ЛК/плана
через UI уже есть, см. §5a) · памятки ГУ-45.

## 5. Подсистема «ПЛАН ПОДВОДА» — над этим работали последним (готово P-1a/P-2a/P-2b)

**Что это:** внешнее Excel-расписание прибытия «ниток» (плановых поездов) в порт,
пер-портовое. Каждую нитку надо **привязать к реальным вагонам** дислокации — тогда
плановое время `PlanMsk` попадает в вагоны и в прогноз Stage 4. Это ядро работы с планом.

### P-1a — парсер плана (влито, PR #38/#39) · `internal/parser/plan/`
Унификация «станция = ДАННЫЕ, не код»:
- `profile.go` — `Profile{PlanCode, OurTerminals, MatchRequiresNaznach}` + встроенные
  `ma`/`nk`; `ResolveProfile` (точка переопределения из БД — позже).
- `registry.go` — `Parser`-интерфейс + реестр кастомных парсеров; `Resolve`/`ParseFile`.
  Добавить станцию = профиль; иной формат листа = свой парсер (самрегистрация), иначе generic.
- `grid.go` — **generic-парсер «новой формы»**: последний лист, снять merge, шапка «N п/п»,
  блоки «План на DD-MM-YYYY», классификация листьев терминалов. **Activ = сумма листьев
  «наших» терминалов** (MA=НМТП+АТТИС, NK=только НМТП). Правило МСК «час≥18→−сутки».
- `nitka.go` — `PlanNitka{Index, IndexPp, PlanJd(без сдвига), PlanMsk(со сдвигом),
  FactMsk, Otkl, Wagons, Activ}`.
- **С.ф.-строки (сборные формирования) ПРОПУСКАЮТСЯ** — отложено (проговорить распределение).
  Свободные нитки (без индекса) не эмитим (эталон).
- Проверено на боевых: Мыс Астафьева 14 ниток/Activ 529, Находка 24/1036.
- **Аномалия:** в Находке Activ>Ваг в паре строк — кривой план РЖД (столбец «ПРОЧИЕ
  ГРУЗЫ» дублирует «Каменный уголь»); эталон суммирует так же — воспроизводим дословно,
  оставляем (решение владельца).

### P-2a — движок матча (влито, PR #38/#39) · `internal/service/planmatch/planmatch.go`
Перенос эталона 1:1 (`gtlogic .../service/plan_utils.go` + `dislocation_plan.go`):
- `Aggregate(records, target)` — 3 карты `ByIndex/ByIndexLast/ByIndexMain`, ключ
  `"<index>|<IdDisl>"`, подгруппы `IndexMain|Naznach|Sms1|GruzpolS` с Quantity.
- `Match(nitki, agg, requiresNaznach)` — выбор лучшей агрегации по **базовому индексу
  (первые 11 из 13 символов)** + скоринг (точность к Activ ≤50 + размер ≤30 + мало
  подгрупп ≤20) + валидация (≤75 ваг; при Activ≥15 → ≥15, иначе ≥1). `NitkaMatch.Vagons`
  — вагоны к простановке.
- **Набор целевых площадок — из `ports.plan_code`** (`DirectoryCache.TargetNaznach`),
  НЕ хардкод. Заменяет эталонный `isMaTargetNaznachOrGruzpolS`.
- Golden-тесты (скоринг/пороги/статус10/MA-vs-NK) ✅.

### P-2b — запись матча + приём файла (влито? PR #41, **ветка `feat/plan-writeback`**)
- `planmatch/apply.go` — `Apply(records, matches, target, now)`: очистка план-полей у
  «наших» (Status≠10) + простановка `IndexPp/PlanMsk/PlanJd`; штамп `UpdatedAt`. +golden-тест.
- `service/plan_process.go` — `PlanProcessor.ProcessFile`: сохранить → `ParseFile` →
  `Aggregate`+`Match` → `Apply` → `ReplaceActual` + `actual.Load`.
- `handler/plan_upload.go` — `POST /api/v1/dislocation/plan/upload` (multipart `file`+`code`),
  за JWT; провод в `server.Build` (guard `dislRepo!=nil`).
- `cmd/planapply` — прогон файла плана по живому `dpport` (как `jsonrun`).

### P-1b — хранение сетки плана для фронта (PR #42, **ветка `feat/plan-nitka-table`**)
- Миграция `000014_plan`: таблицы `plan` (заголовок) + `plan_nitka` (сетка).
  Модель «одна станция = один план» — при загрузке полная замена (delete/insert
  ниток, upsert заголовка) в одной транзакции.
- `domain.Plan/PlanNitka` → `port.PlanRepository` (ReplacePlan/GetPlan) → `gorm/plan.go`.
- `PlanProcessor` после матча сохраняет сетку (`matched`/`matched_wagons` из результата).
- `GET /api/v1/dislocation/plan/:code` — заголовок + нитки JSON для фронта.
- Проверено на dpport: сетка совпадает с разбором, повторный прогон идемпотентен.
  `GET /api/v1/dislocation/plan/:code` смоук-тестирован (curl + фронтенд, см. §5a) —
  отдаёт заголовок+нитки корректно, 404 при отсутствии плана для станции.

### Ключевые ОТКЛОНЕНИЯ от эталона (подтверждены владельцем)
1. **ТА/ТА-Н не берём в матч** (MA/NK) — чужой порт, к которому мы отношения не имеем
   (в эталоне ТА зашит в целевой набор). Набор идёт из `ports.plan_code`, поэтому ТА
   добавится **данными без правки кода**, если станет нашим.
2. **Асимметрия сохранена дословно:** агрегация берёт `Naznach ИЛИ GruzpolS`, а write-back
   сверяет только `Naznach` (эталонный `isTargetNaznachForPlan`); для NK дополнительно
   `Naznach==подгруппа` (`shouldUpdateWagonNK` / `MatchRequiresNaznach`).
3. Статус 10 участвует в агрегации/скоринге, но не застолбляется.

## 5a. Фронтенд загрузки ЛК и плана подвода (влито, PR #43)

Первое наполнение `frontend/` реальными экранами (до этого — только заглушки):

- `frontend/src/app/features/dislocation/` — экран «Дислокация»: `dislocation-api.service.ts`
  (`DislocationApiService`, методы `upload/getStatus/process`) + `dislocation.component.ts`.
  Загрузка xlsx ЛК (`nz-upload`, `nzBeforeUpload` возвращает `false` — сами шлём файл через
  сервис, не штатный XHR) → список принятых файлов + замечания контроля (`block`/`warning`,
  `nz-alert`) → кнопка «Обработать в снимок» (активна только при `ready:true`) → после
  успеха — счётчики `LKProcessResult` в `nz-descriptions`.
- `frontend/src/app/features/plan/` — экран «План подвода»: `plan-api.service.ts`
  (`PlanApiService`, методы `upload(code,file)/getPlan(code)`) + `plan.component.ts`.
  `nz-select` станции (статичный список `ma`/`nk` — под встроенные профили
  `internal/parser/plan/profile.go`; появится профиль из БД — добавить строку сюда) →
  загрузка xlsx плана → сетка ниток (`nz-table`) из `GET /plan/:code`; 404 (план не
  загружен) — пустое состояние, не ошибка.
- `frontend/src/app/core/api/api-error.ts` — общий `apiErrorMessage(err)`, достаёт
  `{error: string}` из `HttpErrorResponse` (см. `internal/handler/response.go`).
- Роутинг (`app.routes.ts`): пути `dislocation`/`plan` исключены из автогенерации
  `PlaceholderComponent` (см. `IMPLEMENTED_PATHS`) и заведены как lazy-routes на реальные
  компоненты; `roles: DISP` — константа теперь экспортируется из `nav.config.ts`.
- Стиль сервисов — 1:1 с `core/auth/auth.service.ts`: `async/await` + `firstValueFrom`,
  не RxJS-подписки; `authInterceptor` сам вешает Bearer на `/api/*`.
- **Проверено вживую на `app.gtport.ru`** (headless-браузер, dev-юзер `disp`/`disp123` из
  `deploy/keycloak/realm-iqport.json`, затем реальными руками владельца): загрузка ЛК
  (НМТП+АТТИС) → «Обработать» → снимок перестроен; план перезалит для `ma` и `nk`. Логи
  backend чистые, 0 ошибок.
- Тестовые фикстуры для повторной проверки — см. §0 (`new_go/*.xlsx`).

## 6. Как продолжить (следующие шаги, по приоритету)

1. **Stage 4 (ProgMsk/ProgJd)** — прогноз прибытия поездов. Механика в
   `gtlogic .../service/enrich_stage4.go`: поезд ЕСТЬ в плане (PlanMsk задан)→ProgMsk=PlanMsk;
   НЕТ → распределение по слотам с учётом `ports.pc_*` (перерабат. способность) и штрафа
   броса. **Зависит только от `PlanMsk`** — может считать и без плана (деградирует).
2. **С.ф. (синонимы)** — распределение сборных формирований по станциям. Отложено,
   **проговорить с владельцем** (station текущей операции, таблица sf, распределение).
   Эталон: `plan_utils.go` функции `detectAndProcessSynonyms`/`distributeWagons...`.
   В логах видно, сколько с.ф.-строк реально пропускается на боевых файлах (`[plan:ma]
   пропущено с.ф.-строк: N`) — ориентир для масштаба задачи.
3. **S2-4** — очереди bros/простои (статусы 4 и 5, разные таблицы). **Потребляют ProgJd.**
4. **Read-API + фронт для просмотра дислокации** — текущий фронтенд умеет только
   загружать (ЛК/план), но не показывать саму сетку вагонов/статусы. Разделы
   «Перестановки», «Грузовая работа» и т.д. в сайдбаре по-прежнему заглушки.
5. Позже: `vagon_operation` трейл, JSON-ingest по HTTP, памятки ГУ-45.

## 7. Память проекта (в `~/.claude/.../memory/` — скопировать!)

Не уедет с репом. Содержит накопленный контекст (частично продублирован здесь):
- `dislocation-pipeline-progress.md` — **детальный** прогресс всех Stage/S2/плана (самый ценный).
- `settings-tables-architecture.md` — 5 слоёв настроечных таблиц; терминал=ОКПО+станция;
  интервалы из `pc_*`; route_speed/is_bam.
- `gtport-data-layer-canon.md` — канон слоя данных (GORM-гибрид, LocalTime без Z, снимки vs vagon_history).
- `gitignore-anchor-binaries.md` — якорить бинарники (`/server` не `server`).
- `MEMORY.md` — индекс памяти.

Если память потеряется — этого HANDOFF + кода + `gtlogic`-эталона достаточно, чтобы
восстановить контекст и продолжить.

## 8. Окружение и команды

- **VPS:** Frankfurt, `147.45.216.229`, юзер `alex` (sudo). Проекты в `/home/alex/projects`.
- **Службы:** nginx (единственный вход из интернета, домен `app.gtport.ru`→фронт),
  docker, tailscaled. **Порты НЕ трогать:** 80/443 (nginx), 3000 (Open WebUI gtport.ru!),
  22, 5432. Приложения слушают только `127.0.0.1`, наружу — через nginx. Backend Go →
  `127.0.0.1:8080`, фронт dev → `:4200` (systemd user-юнит `dpmodule-frontend`).
- **БД:** PostgreSQL, база `dpport`, схема `dpport`, юзер `gtport_app`, пароль в `.env`
  (`PG_PASSWORD`). Миграции: `go run ./cmd/migrate -dir migrations up` (нужен `PG_DSN`/env).
- **systemd-юниты (`~/.config/systemd/user/`):** `dpmodule-backend` (бинарник
  `bin/server`, порт 8080) и `dpmodule-frontend` (`ng serve`, порт 4200). Оба — обычные
  юзер-юниты, `systemctl --user restart|status <имя>`, живой лог — `journalctl --user -u
  <имя> -f`. `dpmodule-backend` подключает секреты через `EnvironmentFile=
  /home/alex/projects/DPmodule/.env` (`PG_PASSWORD=...`, права 600) — **если сервис падает
  с `secret PG_PASSWORD is required`, значит `.env` отсутствует или юнит не перечитан**
  (`systemctl --user daemon-reload` после правки юнита). `config.yaml` уже стоит
  `postgres.enabled: true`, `keycloak.enabled: false` (dev — `/api` без JWT).
- **nginx (`/etc/nginx/sites-available/app.gtport.ru`):** `location /api/` содержит
  `client_max_body_size 50m;` (добавлено 2026-07-10 — дефолт nginx 1M резал xlsx-выгрузки
  ЛК/плана ещё на входе, до бэкенда). Если снова ловите `413` на загрузке файла — это
  первое, что проверить.
- **Dev-Keycloak тестовые юзеры** (`deploy/keycloak/realm-iqport.json`, контейнер
  `dpmodule-keycloak`, `127.0.0.1:8180`): `disp`/`disp123` (роль dispatcher),
  `boss`/`boss123`. Логиниться нужно через `https://app.gtport.ru/login` (same-origin с
  Keycloak через nginx `/realms/`) — заход напрямую на `127.0.0.1:4200` ловит CORS,
  т.к. `environment.*.keycloak.url` = `https://app.gtport.ru`.
- **Команды:**
  ```bash
  go build ./...                 # сборка
  go vet ./...                   # статанализ
  go test ./...                  # тесты (golden — основная страховка переноса)
  # прогон JSON через весь конвейер (пишет в dpport):
  set -a; . ./.env; set +a; go run ./cmd/jsonrun new_go/nmtp.json new_go/attis.json
  # разбор плана (только печать, без БД):
  go run ./cmd/planrun "new_go/Мыс Астафьева.xlsx" ma
  # печать матча по дампу дислокации (JSON []domain.Dislocation, без БД):
  go run ./cmd/planrun "new_go/Мыс Астафьева.xlsx" ma disl.json
  # применить план к живому снимку (пишет в dpport):
  go run ./cmd/planapply "new_go/Мыс Астафьева.xlsx" ma
  ```
- Фронт: Angular 21 + ng-zorro + Keycloak в `frontend/`. Первое реальное наполнение —
  загрузка ЛК/плана (§5a); остальные разделы сайдбара всё ещё `PlaceholderComponent`.

## 9. Ветки/PR на момент среза

- `main` — влиты P-1a+P-2a (PR #39), P-2b (#41), P-1b/хранение сетки (#42), обвязка/
  фронт-шаблон владельца (#40), **фронтенд загрузки ЛК+плана (#43)**. Всё, что описано
  в §4/§5/§5a, уже в `main` обоих клонов (после `git pull`).
- Открытых PR на момент среза нет. Новую работу начинать от свежего `main`:
  `git checkout main && git pull && git checkout -b feat/<задача>`.
