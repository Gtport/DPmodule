# GTport — Структуры данных и схема БД (для ИИ и разработчиков)

> Актуальный справочник по структурам данных GTport. Сгенерирован из Go-моделей
> `vagones.go` (Dislocation) и `vagon_history.go` (VagonHistory, VagonOperation).
> Полный DDL — `migration_dpport_full.sql`. **БД `dpport`, схема `dpport`.**

GTport — рабочий инструмент диспетчера логистического центра, обслуживающего три порта.
Стек: **Go + PostgreSQL + React**. Источник данных — выгрузка дислокации вагонов из АСУ РЖД (SPV4664).

---

## 1. Ключевые соглашения (прочитать первым)

### 1.1. Время — всегда «как есть»
Время хранится и отдаётся **ровно тем числом, что пришло из АСУ** — без перевода зон и UTC.
Тип в Go — `LocalTime` (обёртка над `time.Time`): в JSON и БД **без `Z`**, формат `2006-01-02T15:04:05`.
В БД — `timestamp WITHOUT time zone`. Единая московская шкала АСУ; функции перевода зон **не применяются**.
Имя файла источником времени не считать (у ЛК — МСК, у JSON — Владивосток; содержимое обоих — МСК).

### 1.2. Коды, а не имена
В систему кладутся **коды** (станций, дорог, стран, груза, рода вагона), имена проставляет **обогащение**.
Из JSON коды голые; из Excel ЛК — зашиты в скобках «Имя (код)», извлекаются из скобок.
В БД коды — `text`/`char` (ведущие нули сохраняются), не числовые типы.

### 1.3. Паритет парсеров
`parse_json.go` и `parse_lk.go` дают **один набор полей** модели `Dislocation`.

### 1.4. Источники (АСУ SPV4664)
- **JSON** (основной): плоский массив `[ {…}, {…} ]` (новый формат) либо обёртка
  `data.getReferenceSPV4664Response.vagons` (старый). `ParseJSONFile` определяет формат по первому
  символу (`[` → массив) — см. `extractVagons`. Поля UPPER_SNAKE, голые коды.
- **Excel «ЛК»** (резервный): русские заголовки, коды в скобках, строка заголовка — 4-я.

---

## 2. Поток данных и схема БД

```
Источник (JSON / Excel ЛК)
        │  parse_json.go / parse_lk.go    (только коды, паритет полей)
        ▼
   dislocation  ──────────►  обогащение Stage 1–4  (имена, статусы, план, прогноз)
        │
        ├──────────►  vagon_history     (снимок рейса; trip_key — GENERATED)
        │                   │ 1:N (trip_key, ON DELETE CASCADE)
        │                   ▼
        └──────────►  vagon_operation   (история продвижения в пределах рейса, запрос 601)
```

Три таблицы в схеме **`dpport.dpport`**: `dislocation`, `vagon_history`, `vagon_operation`.
Создание с нуля:
```bash
createdb dpport
psql -d dpport -f migration_dpport_full.sql   # CREATE SCHEMA dpport + 3 таблицы + индексы
```

Этапы обогащения: **Stage 1** станции/операции (имена, дороги, координаты, Status, DateKon);
**Stage 2** marka/ports (имена грузоотпр./получ., SMS, IndexMain, ToGo, RaschMsk);
**Stage 3** Perestanovka; **Stage 4** расписание порта (ProgMsk, DelayProg, Mistake).

---

## 3. dislocation — оперативная дислокация

Текущее состояние вагона. Парсер кладёт коды, обогащение — имена и расчёты.
PK — `id` (детерминированный `vagon/станция/дата`). Строковые поля — `NOT NULL DEFAULT ''`.

#### Основные идентификаторы

| Поле (Go) | Колонка БД | Тип Go | Тип БД | Описание |
|---|---|---|---|---|
| `ID` | `id` | string | text PK |  |
| `Vagon` | `vagon` | string | text |  |
| `Invoice` | `invoice` | string | text |  |
| `InvoiceMain` | `invoice_main` | string | text |  |

#### Индексы поезда

| Поле (Go) | Колонка БД | Тип Go | Тип БД | Описание |
|---|---|---|---|---|
| `Index` | `index` | string | text |  |
| `IndexMain` | `index_main` | string | text |  |
| `IndexLast` | `index_last` | string | text |  |
| `IndexPp` | `index_pp` | string | text |  |

#### Данные погрузки / отправления

| Поле (Go) | Колонка БД | Тип Go | Тип БД | Описание |
|---|---|---|---|---|
| `DateNach` | `date_nach` | *LocalTime | timestamp |  |
| `DateOtpr` | `date_otpr` | *LocalTime | timestamp |  |
| `CodeStationNach` | `code_station_nach` | string | text |  |
| `StationNach` | `station_nach` | string | text |  |
| `DorogaNach` | `doroga_nach` | string | text |  |
| `StrNach` | `str_nach` | string | text |  |
| `Zayavka` | `zayavka` | string | text |  |

#### Грузоотправитель

| Поле (Go) | Колонка БД | Тип Go | Тип БД | Описание |
|---|---|---|---|---|
| `GruzotprOkpo` | `gruzotpr_okpo` | string | text |  |
| `Gruzotpr` | `gruzotpr` | string | text |  |

#### Назначение и грузополучатель

| Поле (Go) | Колонка БД | Тип Go | Тип БД | Описание |
|---|---|---|---|---|
| `CodeStanNazn` | `code_stan_nazn` | string | text |  |
| `Code4StanNazn` | `code4_stan_nazn` | string | text |  |
| `StanNazn` | `stan_nazn` | string | text |  |
| `DorogaNazn` | `doroga_nazn` | string | text |  |
| `StrNazn` | `str_nazn` | string | text |  |
| `GruzpolOkpo` | `gruzpol_okpo` | string | text |  |
| `Gruzpol` | `gruzpol` | string | text |  |
| `GruzpolS` | `gruzpol_s` | string | text |  |
| `Naznach` | `naznach` | string | text |  |
| `Perestanovka` | `perestanovka` | string | text |  |

#### Данные груза

| Поле (Go) | Колонка БД | Тип Go | Тип БД | Описание |
|---|---|---|---|---|
| `CodeCargo` | `code_cargo` | string | text |  |
| `CodeCargoGng` | `code_cargo_gng` | string | text |  |
| `CodeCargoVygr` | `code_cargo_vygr` | string | text |  |
| `CargoS` | `cargo_s` | string | text |  |
| `CargoSms` | `cargo_sms` | string | text |  |
| `CargoGroup` | `cargo_group` | string | text |  |
| `Ves` | `ves` | *float64 | numeric |  |
| `PorozhPriznak` | `porozh_priznak` | string | text |  |
| `FreightExactName` | `freight_exact_name` | string | text |  |
| `GtdNumber` | `gtd_number` | string | text |  |

#### Операции

| Поле (Go) | Колонка БД | Тип Go | Тип БД | Описание |
|---|---|---|---|---|
| `TimeOp` | `time_op` | *LocalTime | timestamp |  |
| `DateOp` | `date_op` | *LocalTime | timestamp |  |
| `DateOpJd` | `date_op_jd` | *LocalTime | timestamp |  |
| `CodeOper` | `code_oper` | string | text |  |
| `Oper` | `oper` | string | text |  |
| `OperS` | `oper_s` | string | text |  |
| `CodeStationOper` | `code_station_oper` | string | text |  |
| `StationOper` | `station_oper` | string | text |  |
| `DorogaOper` | `doroga_oper` | string | text |  |

#### Идентификаторы отправки

| Поле (Go) | Колонка БД | Тип Go | Тип БД | Описание |
|---|---|---|---|---|
| `IdOtprk` | `id_otprk` | string | text |  |
| `Uno` | `uno` | string | text |  |

#### Географические данные

| Поле (Go) | Колонка БД | Тип Go | Тип БД | Описание |
|---|---|---|---|---|
| `Latitude` | `latitude` | string | text |  |
| `Longitude` | `longitude` | string | text |  |
| `Temper` | `temper` | *float64 | numeric |  |

#### Расстояния

| Поле (Go) | Колонка БД | Тип Go | Тип БД | Описание |
|---|---|---|---|---|
| `RasstStanNazn` | `rasst_stan_nazn` | *int | integer |  |
| `RasstOb` | `rasst_ob` | *int | integer |  |
| `RasstStanOp` | `rasst_stan_op` | *int | integer |  |

#### Простои

| Поле (Go) | Колонка БД | Тип Go | Тип БД | Описание |
|---|---|---|---|---|
| `ProstDn` | `prost_dn` | *int | integer |  |
| `ProstCh` | `prost_ch` | *int | integer |  |
| `ProstMin` | `prost_min` | *int | integer |  |

#### Идентификаторы и статусы

| Поле (Go) | Колонка БД | Тип Go | Тип БД | Описание |
|---|---|---|---|---|
| `IdDisl` | `id_disl` | string | text |  |
| `NppVag` | `npp_vag` | *int | integer |  |
| `Status` | `status` | *int | integer |  |
| `IdStatus5` | `id_status5` | string | text |  |
| `IdStatus4` | `id_status4` | string | text |  |
| `DateDostav` | `date_dostav` | *LocalTime | timestamp |  |
| `Delay` | `delay` | *int | integer |  |
| `DelayProg` | `delay_prog` | *int | integer |  |

#### Планы и прогнозы

| Поле (Go) | Колонка БД | Тип Go | Тип БД | Описание |
|---|---|---|---|---|
| `PlanJd` | `plan_jd` | *LocalTime | timestamp |  |
| `PlanMsk` | `plan_msk` | *LocalTime | timestamp |  |
| `ToGo` | `to_go` | *float64 | numeric |  |
| `RaschMsk` | `rasch_msk` | *LocalTime | timestamp |  |
| `ProgMsk` | `prog_msk` | *LocalTime | timestamp |  |
| `Mistake` | `mistake` | *float64 | numeric |  |
| `RaschJd` | `rasch_jd` | *LocalTime | timestamp |  |
| `ProgJd` | `prog_jd` | *LocalTime | timestamp |  |
| `DateKon` | `date_kon` | *LocalTime | timestamp |  |
| `DatePrib` | `date_prib` | *LocalTime | timestamp |  |

#### Маршрут

| Поле (Go) | Колонка БД | Тип Go | Тип БД | Описание |
|---|---|---|---|---|
| `IsBam` | `is_bam` | bool | boolean |  |

#### Собственник и оператор вагона (новые поля)

| Поле (Go) | Колонка БД | Тип Go | Тип БД | Описание |
|---|---|---|---|---|
| `CarOwnerName` | `car_owner_name` | string | text |  |
| `CarOwnerOkpo` | `car_owner_okpo` | string | text |  |
| `CarTenantName` | `car_tenant_name` | string | text |  |
| `CarTenantOkpo` | `car_tenant_okpo` | string | text |  |

#### Клиент и пользовательские поля из словаря marka

| Поле (Go) | Колонка БД | Тип Go | Тип БД | Описание |
|---|---|---|---|---|
| `Client` | `client` | string | text |  |
| `Sms1` | `sms_1` | string | text |  |
| `Sms2` | `sms_2` | string | text |  |
| `Sms3` | `sms_3` | string | text |  |
| `Sprav1` | `sprav_1` | string | text |  |
| `Sprav2` | `sprav_2` | string | text |  |
| `Sprav3` | `sprav_3` | string | text |  |
| `Param1` | `param_1` | string | text |  |
| `Param2` | `param_2` | string | text |  |
| `Param3` | `param_3` | string | text |  |
| `NParam1` | `n_param_1` | string | text |  |
| `NParam2` | `n_param_2` | string | text |  |
| `NParam3` | `n_param_3` | string | text |  |
| `DateVigr` | `date_vigr` | *LocalTime | timestamp |  |
| `PlaceVigr` | `place_vigr` | string | text |  |
| `Frost` | `frost` | *int | integer |  |
| `Info1` | `info_1` | string | text |  |
| `Info2` | `info_2` | string | text |  |
| `Info3` | `info_3` | string | text |  |
| `Color` | `color` | string | text |  |

#### Дополнительные технические поля

| Поле (Go) | Колонка БД | Тип Go | Тип БД | Описание |
|---|---|---|---|---|
| `RodVagUch` | `rod_vag_uch` | string | text |  |
| `Shipments` | `shipments` | string | text |  |
| `History` | `history` | int | integer |  |

#### Служебные временные метки

| Поле (Go) | Колонка БД | Тип Go | Тип БД | Описание |
|---|---|---|---|---|
| `CreatedAt` | `created_at` | LocalTime | timestamp |  |
| `UpdatedAt` | `updated_at` | LocalTime | timestamp |  |

### Не парсятся (заполняет обогащение или флаг приложения)
`IndexMain` (← Index, не NOM_POEZD), `DorogaNach`/`DorogaOper` (← stations.Road, имя),
`Gruzotpr`/`Gruzpol` (← marka/ports, имя). `History` — `int`-флаг запроса истории по API (601), не из АСУ.

---

## 4. Парсеры

### parse_json.go
`JSONVagon` — только маппящиеся поля. Формат определяется по первому символу (`[` → плоский массив,
иначе обёртка). Не парсятся вхолостую: `NOM_POEZD`, `DOR_NACH`, `DOR_RASCH`, `GRUZOTPR`, `GRUZPOL`.
`PorozhPriznak`/`Uno` приходят готовыми (код `1`/`0`; 12 знаков). Даты — ISO без `Z`.

### parse_lk.go (Excel ЛК) — извлечение только кодов
| Способ | Функция | Поля |
|---|---|---|
| Код станции (6 зн.) из скобок | `extractSixDigits` | станции |
| Любой код из скобок | `extractDigitsFromBrackets` | страны, дороги, груз, род вагона, операция |
| Голый код | `getValue` | ГНГ, ОКПО, ТГНЛ, идентификатор отправки |
| Слово → код | `porozhToCode` | «Тип парка (П/Г)»: Порожний→`1`, Груженый→`0` |
| Паддинг до 12 | `padUno` | «Идентификатор накладной» |
| Часы/минуты из «д:ч:м» | `parseProstCh` / 3-й элемент | простой |

Нет в ЛК (только из JSON/эндпоинта): `GtdNumber`, `Zayavka`, `CarOwnerName/Okpo`, `CarTenantName/Okpo`, `FreightExactName`.

---

## 5. vagon_history — снимок рейса

Curated-снимок для отчётов. Не копия `Dislocation`: подмножество + производные.
Создаётся/обновляется в `history_service.go`. `trip_key` — **вычисляемая БД** колонка (см. §6).
Поля упорядочены по жизненному циклу: идентификация → маршрут → груз → собственность →
план/статус → **прибытие → подача → выгрузка → уборка** → метки → служебное.

#### Идентификация / ключи

| Поле (Go) | Колонка БД | Тип Go | Тип БД | Описание |
|---|---|---|---|---|
| `ID` | `id` | string | text PK |  |
| `Vagon` | `vagon` | string | text |  |
| `TripKey` | `trip_key` | int64 | bigint GENERATED |  |
| `InvoiceMain` | `invoice_main` | string | text |  |
| `Invoice` | `invoice` | string | text |  |
| `IndexMain` | `index_main` | string | text |  |
| `IndexPp` | `index_pp` | string | text |  |

#### Отправление / маршрут

| Поле (Go) | Колонка БД | Тип Go | Тип БД | Описание |
|---|---|---|---|---|
| `DateNachD` | `date_nach_d` | *time.Time | timestamp | дата погрузки (ЖД-сутки) |
| `StationNach` | `station_nach` | string | text |  |
| `Gruzotpr` | `gruzotpr` | string | text | имя (из обогащения) |
| `Zayavka` | `zayavka` | string | text |  |
| `StanNazn` | `stan_nazn` | string | text |  |
| `GruzpolS` | `gruzpol_s` | string | text |  |
| `Naznach` | `naznach` | string | text |  |

#### Груз

| Поле (Go) | Колонка БД | Тип Go | Тип БД | Описание |
|---|---|---|---|---|
| `CargoS` | `cargo_s` | string | text |  |
| `CargoGroup` | `cargo_group` | sql.NullString | text |  |
| `FreightExactName` | `freight_exact_name` | string | text | точное наименование |
| `GtdNumber` | `gtd_number` | string | text | номер ГТД |
| `Ves` | `ves` | *float64 | numeric |  |
| `Client` | `client` | string | text |  |

#### Собственность вагона

| Поле (Go) | Колонка БД | Тип Go | Тип БД | Описание |
|---|---|---|---|---|
| `RodVagUch` | `rod_vag_uch` | string | text | код рода вагона (НЕ собственник) |
| `CarOwnerName` | `car_owner_name` | string | text | собственник (имя) |
| `CarOwnerOkpo` | `car_owner_okpo` | string | text | собственник (ОКПО) |
| `CarTenantName` | `car_tenant_name` | string | text | оператор (имя) |
| `CarTenantOkpo` | `car_tenant_okpo` | string | text | оператор (ОКПО) |

#### План / статус / доставка

| Поле (Go) | Колонка БД | Тип Go | Тип БД | Описание |
|---|---|---|---|---|
| `Status` | `status` | *int | integer |  |
| `DateDostav` | `date_dostav` | *time.Time | timestamp |  |
| `PlanMsk` | `plan_msk` | *time.Time | timestamp |  |
| `PlanJd` | `plan_jd` | *time.Time | timestamp |  |
| `Otkl` | `otkl` | string | text | отклонение факт/план |
| `Delay` | `delay` | *int | integer | просрочка доставки, сутки |

#### ПРИБЫТИЕ

| Поле (Go) | Колонка БД | Тип Go | Тип БД | Описание |
|---|---|---|---|---|
| `DatePrib` | `date_prib` | *time.Time | timestamp | дата прибытия (ст.10 — расчётный DateKon) |
| `DatePribD` | `date_prib_d` | *time.Time | timestamp | дата прибытия (только дата) |
| `DateUvPrib` | `date_uv_prib` | *time.Time | timestamp | дата уведомления о прибытии |
| `NomUvPrib` | `nom_uv_prib` | string | text | номер уведомления о прибытии |

#### ПОДАЧА

| Поле (Go) | Колонка БД | Тип Go | Тип БД | Описание |
|---|---|---|---|---|
| `DatePod` | `date_pod` | *time.Time | timestamp | дата подачи на фронт |
| `DateUvPod` | `date_uv_pod` | *time.Time | timestamp | дата уведомления о подаче |
| `NomUvPod` | `nom_uv_pod` | string | text | номер уведомления о подаче |
| `DateGu45Pod` | `date_gu45_pod` | *time.Time | timestamp | дата ГУ-45 (памятка) на подачу — уточнить |
| `NomGu45Pod` | `nom_gu45_pod` | string | text | номер ГУ-45 на подачу |
| `DatePodGu45` | `date_pod_gu45` | *time.Time | timestamp | дата подачи по ГУ-45 — уточнить |
| `PlacePod` | `place_pod` | string | text | место/фронт подачи |

#### ВЫГРУЗКА

| Поле (Go) | Колонка БД | Тип Go | Тип БД | Описание |
|---|---|---|---|---|
| `DateVigr` | `date_vigr` | *time.Time | timestamp | дата-время выгрузки (статус 12) |
| `DateVigrD` | `date_vigr_d` | *time.Time | timestamp | дата выгрузки (ЖД-сутки) |
| `DateVigrGu45` | `date_vigr_gu45` | *time.Time | timestamp | дата ГУ-45 при выгрузке |
| `PlaceVigr` | `place_vigr` | string | text | порт выгрузки (статус 12) |

#### УБОРКА

| Поле (Go) | Колонка БД | Тип Go | Тип БД | Описание |
|---|---|---|---|---|
| `DateUbor` | `date_ubor` | *time.Time | timestamp | дата уборки с фронта |
| `DateGu45Ubor` | `date_gu45_ubor` | *time.Time | timestamp | дата ГУ-45 на уборку — уточнить |
| `NomGu45Ubor` | `nom_gu45_ubor` | string | text | номер ГУ-45 на уборку |
| `DateUborGu45` | `date_ubor_gu45` | *time.Time | timestamp | дата уборки по ГУ-45 — уточнить |

#### Метки / прочее

| Поле (Go) | Колонка БД | Тип Go | Тип БД | Описание |
|---|---|---|---|---|
| `Frost` | `frost` | *int | integer | признак заморозки |
| `Shipments` | `shipments` | string | text |  |
| `Info1` | `info_1` | string | text |  |
| `Info2` | `info_2` | string | text |  |
| `Info3` | `info_3` | string | text |  |
| `Sms1` | `sms_1` | string | text |  |
| `Sms2` | `sms_2` | string | text |  |
| `Sms3` | `sms_3` | string | text |  |
| `Color` | `color` | string | text |  |

#### Служебное

| Поле (Go) | Колонка БД | Тип Go | Тип БД | Описание |
|---|---|---|---|---|
| `CreatedAt` | `created_at` | time.Time | timestamp |  |
| `UpdatedAt` | `updated_at` | time.Time | timestamp |  |

### Производные / особые поля
- `trip_key` — `GENERATED ALWAYS AS (vagon::bigint*100000 + (date_nach_d::date - DATE '1970-01-01')) STORED`.
- `DateNachD`/`DatePribD` — `ExtractDateOnly` (отбрасывает время).
- `DateVigrD` — **ЖД-сутки**: час ≥ 18 → +1 день, затем дата (`ExtractDateOnlyJd`).
- `DatePrib` — для статуса 10 берётся расчётный `DateKon`, иначе `DatePrib`.
- Портовые поля (прибытие/подача/выгрузка/уборка): уведомления, ГУ-45 — источник **«Порт / ручной ввод»**, не АСУ.
  Поля с «уточнить» (`DateGu45Pod` vs `DatePodGu45`; `DateGu45Ubor` vs `DateUborGu45`) сверить с регламентом порта.

---

## 6. vagon_operation — история продвижения (в пределах рейса)

Запрос 601 — поток операций (~200 на рейс за ~6 дней). Хранится **только текущий рейс** на вагон.

```sql
CREATE TABLE dpport.vagon_operation (
    trip_key     bigint     NOT NULL REFERENCES dpport.vagon_history(trip_key) ON DELETE CASCADE,
    date_op      timestamp  NOT NULL,
    kop_vmd      varchar(3),
    stan_op      char(6),                -- ведущие нули сохранены
    index_poezd  varchar(15),            -- NULL, если поезда нет («000…0»)
    PRIMARY KEY (trip_key, date_op)
);
```

### trip_key — детерминированный ключ рейса
```
trip_key = vagon * 100000 + (date_nach_d − DATE '1970-01-01')   -- дней от эпохи
```
Монотонный, `bigint`. В `vagon_history` вычисляет БД (`GENERATED`), в Go — `models.TripKey(vagon, dateNachD)`
повторяет ту же формулу. **Проверено**: значения БД и Go совпадают бит-в-бит. Расхождение всплыло бы через FK.

### Механика перезаписи
Повторный запрос (например, смена статуса на 10) → `VagonOperationRepository.ReplaceForTrip`:
в одной транзакции `DELETE по trip_key` + батч-`INSERT`. Тот же `trip_key` затирает прежний набор без дублей.
Новый рейс → новый `trip_key` → старые операции уходят по `ON DELETE CASCADE`.

Парсинг: `Parse601(raw) → []VagonOperation` (индекс «000…0» → `nil`, дата `2006-01-02 15:04:05` без зоны).

### Объём
~100k вагонов × ~200 операций = **~20 млн строк, ~1.5 ГБ**, стабильно. Поиск — один seek по `(trip_key, date_op)`.

---

## 7. Ключевые оговорки (caveats)

| Тема | Суть |
|---|---|
| `IndexMain` | Источник `INDEX_POEZD` (не `NOM_POEZD`); заморожен при первом появлении; заполняет обогащение. |
| `Gruzotpr`/`Gruzpol` | Хранят **ИМЯ** (`marka.Shipper` / `ports.Organisation`); 4-значный код из АСУ не парсится. |
| `RodVagUch` | **Код рода вагона**, НЕ собственник. Собственник — `CarOwnerName`/`CarOwnerOkpo`. |
| `DateKon` vs `DatePrib` | `DATE_KON` («окончание рейса») недостоверен — не заводится. Прибытие = `DatePrib` (`DATE_PRIB`, совпадение JSON↔Excel 91/91). |
| `Dislocation.History` | `int`-флаг запроса истории по API (601). `VagonHistory.History` (строковый лог) **удалён** — операции теперь в `vagon_operation`. |
| Порт-специфичные | `Naznach`, `GruzpolS`, `CargoGroup` завязаны на причалы (УТ-1/АЭ/ГУТ-2) — в универсальной версии вынести в справочник. |
| Время | Все даты — `timestamp` без зоны, без `Z` (единая шкала АСУ, без перевода зон). Канон — `LocalTime` **везде**. `VagonHistory`/`VagonOperation` сейчас на `*time.Time` — переводятся на `LocalTime` при переносе (решение зафиксировано, см. `PROJECT_INSTRUCTIONS.md`). |

---

*Генерируется из Go-моделей и `migration_dpport_full.sql`. При изменении структур обновлять вместе с кодом.*
