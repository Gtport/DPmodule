# Настроечные таблицы DPmodule — памятка для человека

Как менять поведение системы **без программиста и без пересборки** — через настроечные
таблицы в БД `dpport`. Для владельца-логиста; SQL можно копировать как есть.

> Парные документы: `STRUCTURE.md` (карта кода), `TARGET.md` (целевая модель данных),
> `HARDCODE_INVENTORY.md` (что ещё осталось в коде и переедет в настройки).

---

## Пять правил (прочитать первыми)

1. **Настройки — это ДАННЫЕ, не код.** Меняются обычным SQL, пересобирать ничего не надо.
2. **Backend читает настройки в память ОДИН раз при старте.** После любой правки —
   **перезапустить backend**, иначе изменение не подхватится:
   ```bash
   systemctl --user restart dpmodule-backend
   ```
3. **Подключение к БД:** `psql -d dpport` (под пользователем `alex` — суперпользователь,
   пароль не нужен).
4. **Правь по циклу:** сначала `SELECT` (посмотреть как есть) → `UPDATE/INSERT` → снова
   `SELECT` (убедиться) → рестарт → проверить в UI/логе.
5. **Все настроечные таблицы принадлежат `gtport_app`.** Обычные правки (INSERT/UPDATE
   строк) делать можно. А вот **новые таблицы/колонки — только миграцией** (см. последний
   раздел), иначе приложение их не увидит.

---

## Карта таблиц: что где настраивается

| Таблица | Что задаёт | Когда трогать |
|---|---|---|
| `ports` | Терминалы: ОКПО, станция, «имя порта» (`name_s`), `plan_code`, **перерабатывающая способность `pc_*`**, вкл/выкл | Добавить порт, сменить ёмкость, включить/выключить |
| `plan_profile` | Портрет **станции** плана: режим (плановая/бесплановая), поправочный коэффициент, «наши» терминалы, правило матча | Новая станция, смена режима/коэффициента |
| `nitka_schedule` | **Расписание ниток станции** (слоты прибытия) — потолок для всех портов | Добавить/поменять расписание |
| `client_settings` | Общие пороги приёма (свежесть дислокации, потеря данных, рассогласование АСУ) | Подкрутить гарды |
| `data_source` | Каналы ввода (ЛК, АСУ): вкл/выкл, URL, авторизация, маркеры файла | Включить/настроить забор АСУ |
| `sf` | Синонимы станций формирования для с.ф. | Новый синоним/станция формирования |
| `stations`, `route_speed`, `naznach_station` | Справочники обогащения (названия станций, скорости хода, перестановки назначения) | Редко; обычно сидятся миграцией |

---

## Частые задачи (copy-paste)

### 1. Перерабатывающая способность терминала (`pc_*`, вагонов/сутки)
Влияет на прогноз Stage 4 (интервал поездов = `вагонов × 24 / pc_рода`).
```sql
-- посмотреть
SELECT code, name_s, pc_coal, pc_metal, pc_other, pc_total FROM dpport.ports ORDER BY code;
-- изменить (пример: ГУТ-2 уголь 170 → 180)
UPDATE dpport.ports SET pc_coal = 180, pc_total = 180 + COALESCE(pc_metal,0) + COALESCE(pc_other,0)
 WHERE code = 'GUT';
```

### 2. Добавить / выключить терминал (`ports`)
Идентификация терминала — по паре **(ОКПО грузополучателя + станция назначения текстом)**.
```sql
-- выключить терминал (перестанет участвовать в обработке)
UPDATE dpport.ports SET enabled = false WHERE code = 'YT';
-- добавить новый (пример-шаблон; заполнить своими значениями)
INSERT INTO dpport.ports (okpo, location, organisation, name_s, name, code, plan_code,
                          station_code, pc_coal, pc_total, enabled, sort_order)
VALUES (1234567, 'НОВАЯ СТАНЦИЯ', 'ООО "НОВЫЙ ПОРТ"', 'НП', 'Новый порт', 'NP', NULL,
        '990000', 200, 200, true, 50);
```

### 3. Станция плана: режим, коэффициент, «наши» терминалы (`plan_profile`)
Ключ — `station_code` (у терминалов одной станции он общий: Мыс Астафьева `985702`,
Находка `984700`). `our_terminals` — ключевые слова колонок плана, что идут в `Activ`.
```sql
SELECT * FROM dpport.plan_profile;
-- поправочный коэффициент станции (для бесплановой, см. п.5)
UPDATE dpport.plan_profile SET correction_coef = 0.9 WHERE station_code = '985702';
-- поменять "наши" терминалы станции
UPDATE dpport.plan_profile SET our_terminals = '["НАХОДКИНСКИЙ","НМТП","АТТИС"]'::jsonb
 WHERE station_code = '985702';
```

### 4. Расписание ниток станции (`nitka_schedule`)
Слоты — **потолок прибытия станции**, общий для ВСЕХ портов (станция больше физически
не примет). Занятость считается по всем ниткам, не только «нашим».
```sql
SELECT slot_time FROM dpport.nitka_schedule WHERE station_code='985702' ORDER BY sort_order;
-- добавить слот
INSERT INTO dpport.nitka_schedule (station_code, slot_time, sort_order)
VALUES ('985702', '23:15', 10) ON CONFLICT DO NOTHING;
-- убрать слот
DELETE FROM dpport.nitka_schedule WHERE station_code='985702' AND slot_time='23:15';
-- задать расписание Находки (984700) целиком — пример
INSERT INTO dpport.nitka_schedule (station_code, slot_time, sort_order) VALUES
  ('984700','02:00',1),('984700','07:30',2),('984700','14:00',3),('984700','20:30',4)
ON CONFLICT DO NOTHING;
```

### 5. Бесплановая станция (прогноз только из ёмкости)
У станции **нет плана** → прогноз строится из перерабатывающей способности:
не подводить в сутки больше `pc_рода × correction_coef`.
```sql
-- пометить станцию как бесплановую + задать коэффициент (расписание для неё не нужно)
UPDATE dpport.plan_profile SET mode = 'capacity', plan_code = NULL, correction_coef = 0.85
 WHERE station_code = '990000';
-- или завести новую бесплановую станцию
INSERT INTO dpport.plan_profile (station_code, station_name, mode, correction_coef)
VALUES ('990000', 'НОВАЯ СТАНЦИЯ', 'capacity', 0.85) ON CONFLICT (station_code) DO NOTHING;
```
`mode`: `planned` — есть расписание, Stage 4 раскладывает по слотам; `capacity` — плана нет,
прогноз из `pc_* × correction_coef`.

### 6. Пороги приёма (`client_settings.ingest_policy`)
Один синглтон (`id=1`), JSON. Меняем точечно через `jsonb_set`.
```sql
SELECT jsonb_pretty(ingest_policy) FROM dpport.client_settings WHERE id=1;
-- гард свежести плана: не грузить план, если дислокация старше N минут (сейчас 60)
UPDATE dpport.client_settings
   SET ingest_policy = jsonb_set(ingest_policy, '{plan,plan_max_disl_age_minutes}', '90')
 WHERE id=1;
-- порог рассогласования меток АСУ между клиентами, минут (сейчас 2; 0 — выключить гард)
UPDATE dpport.client_settings
   SET ingest_policy = jsonb_set(ingest_policy, '{dislocation,max_source_skew_minutes}', '3')
 WHERE id=1;
```
Ключевые пороги: `plan.plan_max_disl_age_minutes` (гард загрузки/пересчёта плана),
`dislocation.max_source_skew_minutes` (сверка АСУ-клиентов), `dislocation.max_data_loss_pct`
(защита от резкой усушки снимка), `dislocation.max_staleness_minutes`.

### 7. Источник дислокации из АСУ (`data_source`, `id='asu'`)
```sql
SELECT id, enabled, config->>'base_url', config->>'auth_header' FROM dpport.data_source WHERE id='asu';
-- включить забор (URL — реальный; insecure_tls=true для самоподписанного серта на IP)
UPDATE dpport.data_source
   SET config = config
         || jsonb_build_object('base_url','https://<хост-АСУ>:8443/api/v1')
         || jsonb_build_object('insecure_tls', true),
       enabled = true
 WHERE id='asu';
-- выключить забор
UPDATE dpport.data_source SET enabled=false WHERE id='asu';
```
Секрет ключа `X-API-Key` — **не в БД**, а в env процесса: строка `ASU_TOKEN=<значение>` в
`/home/alex/projects/DPmodule/.env` (файл в `.gitignore`), подхватывается при рестарте.

### 8. Синонимы станций формирования для с.ф. (`sf`)
Нормализует варианты написания синонима → каноническая станция + потолок вагонов.
```sql
SELECT sinonim, station, quantity FROM dpport.sf ORDER BY sinonim;
INSERT INTO dpport.sf (sinonim, station, quantity) VALUES ('ХАБАР-К III','ХАБАРОВСК II',50)
ON CONFLICT DO NOTHING;
```

---

## Применить и проверить

1. **Рестарт** (обязательно после правки настроек):
   ```bash
   systemctl --user restart dpmodule-backend
   systemctl --user status dpmodule-backend --no-pager | head -3
   ```
2. **Проверить в логе**, что настройки загрузились без ошибок:
   ```bash
   journalctl --user -u dpmodule-backend --since '-1 min' | grep -iE 'cache loaded|plan profiles|error'
   ```
3. **Проверить в UI / API**: статус-панель (`/api/v1/dislocation/status`), журнал
   (`/api/v1/dislocation/journal`), раздел «План подвода».

---

## Что НЕ трогать руками

- `schema_migrations` — служебная таблица версий; ведётся инструментом миграций.
- `dislocation`, `dislocation_new` — снимок дислокации; пересобирается движком, не руками.
- `vagon_history`, `event_journal` — бизнес-история и аудит; пишутся приложением.
- Не менять **владельца** настроечных таблиц (должен оставаться `gtport_app`).

---

## Данные vs миграция

- **Правка значений** (пороги, ёмкость, вкл/выкл, расписание, коэффициент) — прямой SQL
  по этой памятке. Рестарт — и всё.
- **Изменение структуры** (новая колонка/таблица, новый тип настройки) — только **миграцией**
  (`migrations/NNNNNN_*.sql`, применяется `cmd/migrate`), с передачей владения `gtport_app`.
  Это задача с кодом → отдельная ветка + PR.
- Начальные значения для нового клиента (silo) приезжают **сидом в миграции**, не вручную.
