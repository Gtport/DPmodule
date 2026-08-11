---
name: naznach-gtport
description: Сверить и выровнять назначение (naznach) в боевом снимке дислокации по выгрузке gtport «Полная повагонка …xlsx». Использовать, когда просят «поставить правильные naznach по выгрузке», «сверить перестановки с gtport», «почему у вагона не тот терминал», либо когда владелец приносит свежий файл повагонки как эталон.
---

# Выравнивание `naznach` по выгрузке gtport

Эталон — gtport (жёсткое правило проекта). Выгрузка «Полная повагонка ДД.ММ.ГГ ЧЧ-ММ.xlsx»
(лист **`Дислокация`**) — срез его снимка на момент времени. Наш `naznach` в
`dpport.dislocation` должен совпадать с её колонкой **`Назначение`**.

⚠️ **Это правка боевых данных.** Порядок обязателен: сверка → прогон на `ROLLBACK`
→ подтверждение владельца → бэкап → `COMMIT` → рестарт бэкенда → проверка.

## Что надо знать ДО правки (иначе правка не встанет)

### 1. `naznach` живёт в RAM, а не в таблице
Снимок держится в `ActualCache` ([actual.go](../../../internal/service/actual.go)), а
`naznach` переносится carry-over'ом из RAM в каждый новый батч
([carryover.go:124](../../../internal/service/carryover.go#L124), «операторские решения —
снимок вернее потока»). Значит:

> **UPDATE в БД без рестарта бэкенда будет затёрт ближайшим батчем АСУ/ЛК.**

После `COMMIT` — обязательно `systemctl --user restart dpmodule-backend`: на старте
`main.go:188` делает `actualCache.Load()` и поднимает новые значения в RAM.
Батчи АСУ идут **каждые 10 минут** (:05, :15, :25, :35, :45, :55 по Москве) — окно между
`COMMIT` и рестартом надо держать секундами.

### 2. Пустой `naznach` поставить нельзя — он самозаполняется
`applyMarkaEnrichment` на КАЖДОМ батче видит пустой `naznach` и ставит `gruzpol_s`
([marka.go:48](../../../internal/service/marka.go#L48) → [marka.go:245](../../../internal/service/marka.go#L245)).
Проверено на живых данных 11.08.2026: 15 вагонов, выставленных в пусто, вернулись к
`ГУТ-2` первым же батчем через 80 секунд.

Инвариант снимка: **`naznach` непуст у всех вагонов**. Законная пустота одна —
нерезолвнутый порт (нет ОКПО / нет станции назначения / пары нет в `ports`,
[enrich.go:158](../../../internal/service/enrich.go#L158)), и тогда пуст и `gruzpol_s`.

### Пустое «Назначение» у gtport — как правило, дефект ЭТАЛОНА, а не наш
`naznach` — копия `gruzpol_s` (и у нас, и в gtport: `enrich_stage2.go
enrichFromNaznachStation`). Пустое назначение при непустом получателе невозможно, поэтому
пустая колонка у gtport значит «у него не определился ПОЛУЧАТЕЛЬ». А получатель там
определяется **один раз — при первом появлении вагона**:

```go
// gtlogic enrich_stage2.go SecondEnrichment
if existingRecord, found := s.FindVagonInActual(newRec.Vagon); found {
    s.enrichFromActual(newRec, existingRecord)   // carry-over, справочники не трогаются
} else {
    s.enrichFromDictionaries(newRec)             // ports → marka → naznach_station: ОДИН раз
}
```

Пришла запись битой в момент рождения — пустой получатель едет carry-over'ом до конца
рейса, второй попытки нет. Типичный триггер: станция отправления вне справочника →
ранний `return` в `enrichStationData` → не заполнено имя станции назначения →
`enrichFromPorts` выходит по пустому `StanNazn`. Побочный признак в выгрузке: у таких
строк грузоотправитель и грузополучатель стоят **сырыми кодами потока** (`1112`, `6624`
вместо имён), пустые груз/клиент/сегмент.

**У нас этого класса дефектов нет:** `applyMarkaEnrichment` повторяет попытку на каждом
батче, пока `naznach` пуст, плюс «Обновить справочники». Поэтому прежде чем «выравнивать»,
проверить сырой поток (`_data/lk_raw/report_<ОКПО>.json`) и нашу `vagon_history`: если у
нас терминал определён и он согласован с `ports`, **правы мы** — выравнивать нельзя.
Боевой случай 11.08.2026 — 15 вагонов ШУШАРЫ (код `033004`, в `stations` весь диапазон
00xxxx–05xxxx отсутствует).

⚠️ Историю строки истории и снимка **нельзя соединять по `id`**: у рейсов, родившихся без
станции отправления, id истории вида `temp_<число>`, а в снимке уже настоящий. Джойнить
по `trip_key` или по номеру вагона — иначе LEFT JOIN даёт NULL, который легко прочитать
как «в истории пусто» (наступили на это 11.08.2026).

### 3. `'0'` — это не ошибка, а статус 6
`gruzpol_s='0'` и `naznach='0'` движок ставит намеренно при ПЕРЕХОДЕ вагона в статус 6
(порожний в пути, к нам не доедет) — §3.16, [stage2.go:40](../../../internal/service/stage2.go#L40).
Обнуление живёт только в снимке, в `vagon_history` у вагона честный терминал.
В gtport такие вагоны показаны с обычным терминалом. **Выравнивать их — значит вернуть
вагон в выборки терминала.** Спрашивать владельца отдельно, по умолчанию не трогать.

## Порядок

### 1. Выгрузку → CSV
Матч с БД идёт по **номеру вагона** (в снимке он уникален; проверить, что дублей нет).

```bash
cd <scratchpad>
python3 - <<'PY'
import openpyxl, csv
wb = openpyxl.load_workbook('/home/alex/projects/Полная повагонка ДД.ММ.ГГ ЧЧ-ММ.xlsx')
ws = wb['Дислокация']
rows = list(ws.iter_rows(values_only=True))
idx = {h: i for i, h in enumerate(rows[0])}
cols = ['Номер вагона','Станция отправления','Станция назначения',
        'Получатель','Назначение','Перестановка','Начало рейса','Индекс поезда']
with open('gt.csv','w',newline='') as f:
    w = csv.writer(f); w.writerow(cols)
    for r in rows[1:]:
        w.writerow(['' if r[idx[c]] is None else r[idx[c]] for c in cols])
print('строк:', len(rows)-1)
PY
```

Колонки эталона: `Получатель` = наш `gruzpol_s`, `Назначение` = наш `naznach`,
`Перестановка` = `Получатель/Назначение` заполнена, только когда они разошлись.

⚠️ `openpyxl` в `read_only=True` врёт про размеры листа (`max_row=1`) — грузить обычным режимом.

### 2. Сверка
```sql
CREATE TEMP TABLE gt(vagon text, station_nach text, stan_nazn text, poluch text,
                     naznach text, perest text, nach text, idx text);
\copy gt FROM 'gt.csv' WITH (FORMAT csv, HEADER true)
UPDATE gt SET naznach = coalesce(naznach,'');   -- ⚠️ пустое поле CSV приходит как NULL,
UPDATE gt SET poluch  = coalesce(poluch,'');    --    а колонки в dislocation NOT NULL

-- контроль матча
SELECT (SELECT count(*) FROM gt)                                                  AS "в выгрузке",
       (SELECT count(*) FROM dpport.dislocation)                                  AS "в снимке",
       (SELECT count(*) FROM dpport.dislocation d JOIN gt g ON g.vagon=d.vagon)   AS "сматчилось",
       (SELECT count(*) FROM (SELECT vagon FROM gt GROUP BY 1 HAVING count(*)>1) t) AS "дубли в выгрузке";

-- расхождения с разбором природы
SELECT d.station_nach, d.gruzpol_s, d.naznach AS db, g.naznach AS gt,
       g.perest, d.status, d.oper_s, count(*)
FROM dpport.dislocation d JOIN gt g ON g.vagon=d.vagon
WHERE d.naznach IS DISTINCT FROM g.naznach
GROUP BY 1,2,3,4,5,6,7 ORDER BY 1;
```

### 3. Классифицировать расхождения и доложить владельцу
Не «N вагонов разошлись», а по природе — от этого зависит, что вообще применимо:

| Признак | Природа | Что делать |
|---|---|---|
| `naznach ≠ gruzpol_s` у gtport (`Перестановка` заполнена) | перестановка диспетчера, у нас её нет | **выравнивать**, carry-over удержит |
| у нас перестановка из справочника, в gtport её нет | `naznach_station` шире эталона | выравнивать вагон; заодно проверить строку справочника |
| наш `status=6`, у нас `'0'` | §3.16, штатное обнуление | **не трогать** без отдельного решения |
| у gtport пусто, у нас терминал | почти наверняка **дефект эталона** (одноразовое обогащение) | не выравнивать: пустое не встанет, и правы, скорее всего, мы |

Расхождение только у нескольких вагонов одной станции при совпадении остальных — это
ручное решение диспетчера, а не правило: справочник `naznach_station` не трогать.

### 4. Прогон на `ROLLBACK`, затем применение
```sql
BEGIN;
SELECT naznach, count(*) FROM dpport.dislocation GROUP BY 1 ORDER BY 2 DESC;   -- ДО
UPDATE dpport.dislocation d SET naznach = g.naznach
  FROM gt g WHERE g.vagon = d.vagon AND d.naznach IS DISTINCT FROM g.naznach
  AND d.status <> 6;                       -- сузить WHERE ровно под согласованные группы
SELECT naznach, count(*) FROM dpport.dislocation GROUP BY 1 ORDER BY 2 DESC;   -- ПОСЛЕ
ROLLBACK;
```
Показать владельцу «до/после» и число строк. После подтверждения — то же самое с бэкапом
и `COMMIT`:
```sql
BEGIN;
CREATE TABLE IF NOT EXISTS dpport.naznach_fix_backup_ГГГГММДД AS
  SELECT d.vagon, d.id, d.naznach AS naznach_old, g.naznach AS naznach_new, now() AS fixed_at
  FROM dpport.dislocation d JOIN gt g ON g.vagon=d.vagon
  WHERE d.naznach IS DISTINCT FROM g.naznach AND <тот же фильтр>;
UPDATE dpport.dislocation d SET naznach = g.naznach, updated_at = now()
  FROM gt g WHERE g.vagon = d.vagon AND d.naznach IS DISTINCT FROM g.naznach AND <тот же фильтр>;
COMMIT;
ALTER TABLE dpport.naznach_fix_backup_ГГГГММДД OWNER TO gtport_app;   -- psql ходит под alex
```

### 5. Рестарт и проверка
```bash
systemctl --user restart dpmodule-backend
sleep 3 && journalctl --user -u dpmodule-backend --no-pager -n 40 | grep -iE "actual cache loaded|error|panic"
```
Ждём `actual cache loaded {"vagons": N}` с тем же N, что в снимке.

**Проверять надо ПОСЛЕ следующего батча АСУ** (до 10 минут) — только он докажет, что
значение пережило carry-over, а не просто лежит в таблице:
```sql
SELECT b.naznach_new, d.naznach AS сейчас, count(*)
FROM dpport.naznach_fix_backup_ГГГГММДД b JOIN dpport.dislocation d ON d.vagon=b.vagon
GROUP BY 1,2;
SELECT id, event_type, source, created_at FROM dpport.event_journal
WHERE event_type='disl_update' ORDER BY created_at DESC LIMIT 3;
```
Значение уехало обратно — значит группа была из «неустойчивых» (пустой `naznach`),
и чинить надо не `naznach`.

## Что скил НЕ делает
- **Не трогает `vagon_history`.** Там `naznach` пишется один раз при INSERT и кормит
  отчёты «Прибывшие»/«Погрузка». У уже прибывших/выгруженных вагонов (статусы 10/12)
  после правки снимка история останется со старым терминалом — сказать об этом владельцу
  и решать отдельно.
- **Не правит `naznach_station`.** Справочник меняет назначение всем будущим вагонам пары
  станций; выравнивание по выгрузке — про конкретные вагоны.
- **Не правит получателя (`gruzpol_s`) и `ports`.** Это отдельная задача про атрибуцию.

## Часы: две шкалы, не путать
Приложение штампует `created_at`/`updated_at` **московским naive** через `clock.Now()`,
а `now()` в psql даёт время сервера (`Europe/Berlin`, на час раньше). Строки, «обновлённые
в будущем» относительно `now()`, — это норма и признак того, что запись переписал батч,
а не ваш `UPDATE`.

## История применения
- **11.08.2026**, выгрузка `Полная повагонка 11.08.26 07-12.xlsx` (4860 строк, снимок 4843,
  дублей нет). Расхождений 28: 8 ЧЕЛУТАЙ + 1 ПЕТРОВСКИЙ ЗАВОД `АЭ→ГУТ-2` (встали),
  4 ТЕРЕНТЬЕВСКАЯ статуса 6 `'0'→АЭ` (встали, по решению владельца),
  15 ШУШАРЫ `ГУТ-2→пусто` (**вернулись сами** батчем АСУ — доказательство п. 2).
  Бэкап — `dpport.naznach_fix_backup_20260811`.
  Разбор ШУШАРЫ доведён до причины: в сыром потоке `GRUZPOL='6624'`,
  `GRUZPOL_OKPO='01126022'`, `NAIM_STAN_NAZN='МЫС АСТАФЬЕВА'`; в выгрузке gtport в колонке
  «Грузополучатель» стоит ровно `6624` — справочник `ports` по записи не отработал ни разу,
  а второй попытки эталон не делает. У нас те же вагоны атрибутированы верно (`ГУТ-2`
  в снимке и в истории, `gruzotpr='ООО «ВОСХОД»'`) — правки не требуют.
