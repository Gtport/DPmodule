-- ============================================================================
--  000047_vagon_delay — память о задержках вагонов (эпизоды простоя/бросания).
--
--  Одна строка = один эпизод: «вагон стоял на станции с date_from по date_to».
--  kind: 4 — долгий простой в пути (статус 4), 5 — брошен (статус 5, code_oper=92).
--  Эскалация: если простаивающий вагон бросили на той же станции, эпизод НЕ
--  закрывается — kind затирается 4 → 5 (решение владельца), date_from остаётся
--  началом стоянки. Обратного понижения 5 → 4 нет.
--
--  Ведёт reconcile-слой конвейера (`applyDelays`, после Stage 4, рядом с bros):
--  вагон вошёл в статус 4/5 → открыть эпизод (date_from = time_op — фактическое
--  начало стоянки, включая «допороговое» время); стоит на той же станции →
--  эпизод продолжается (обновляются group_key/индексы); станция сменилась или
--  статус ушёл → закрыть (date_to, hours); вагон пропал из выгрузки → закрыть
--  моментом пересбора. Открытый эпизод — date_to IS NULL, не больше одного на
--  вагон (частичный уникальный индекс).
--
--  Отличие от bros: там оперативный снимок ПОЕЗДОВ для диспетчера (журнал,
--  подъём, отчёт по кодам), здесь — аналитическая память ВАГОНА: полная картина
--  задержек рейса. Связь между ними — group_key (= id_status4/id_status5,
--  `index|code_station_oper|time_op`); у вагонов без индекса ключ пуст, эпизод
--  всё равно пишется. Привязка к рейсу — date_nach_d (как в vagon_history).
--
--  Время — Московское naive (жёсткий инвариант): все колонки — timestamp
--  without time zone, штампы через clock.Now().
--
--  Идемпотентно (CREATE TABLE IF NOT EXISTS), только добавляющая.
-- ============================================================================

SET search_path TO dpport;

CREATE TABLE IF NOT EXISTS dpport.vagon_delay (
    id           bigserial    PRIMARY KEY,
    vagon        varchar(20)  NOT NULL,
    date_nach_d  timestamp,                        -- дата начала рейса (привязка к vagon_history)
    kind         smallint     NOT NULL,            -- 4 = долгий простой, 5 = брошен
    group_key    varchar(120) NOT NULL DEFAULT '', -- id_status4/5 (index|code_station_oper|time_op)
    index_poezd  varchar(50)  NOT NULL DEFAULT '', -- текущий индекс поезда
    index_main   varchar(50)  NOT NULL DEFAULT '', -- «родительский» индекс (ключ плана)
    station_code varchar(20)  NOT NULL DEFAULT '', -- код ЕСР станции стоянки
    station_name varchar(120) NOT NULL DEFAULT '',
    doroga       varchar(50)  NOT NULL DEFAULT '',
    date_from    timestamp    NOT NULL,            -- начало стоянки (time_op)
    date_to      timestamp,                        -- конец эпизода; NULL = стоит сейчас
    hours        numeric(8,1),                     -- длительность, часы (при закрытии)
    created_at   timestamp    NOT NULL DEFAULT now(),
    updated_at   timestamp    NOT NULL DEFAULT now()
);

COMMENT ON TABLE  dpport.vagon_delay IS 'Задержки вагонов: эпизоды долгого простоя (статус 4) и бросания (статус 5) — где стоял, с какого по какое, сколько часов';
COMMENT ON COLUMN dpport.vagon_delay.vagon        IS 'Номер вагона';
COMMENT ON COLUMN dpport.vagon_delay.date_nach_d  IS 'Дата начала рейса (привязка к vagon_history)';
COMMENT ON COLUMN dpport.vagon_delay.kind         IS 'Вид задержки: 4 — долгий простой в пути, 5 — брошен';
COMMENT ON COLUMN dpport.vagon_delay.group_key    IS 'Ключ агрегации id_status4/5 — связь с bros и вагонами той же группы';
COMMENT ON COLUMN dpport.vagon_delay.index_poezd  IS 'Текущий индекс поезда';
COMMENT ON COLUMN dpport.vagon_delay.index_main   IS 'Родительский индекс (ключ сопоставления с планом)';
COMMENT ON COLUMN dpport.vagon_delay.station_code IS 'Код ЕСР станции стоянки';
COMMENT ON COLUMN dpport.vagon_delay.station_name IS 'Имя станции стоянки';
COMMENT ON COLUMN dpport.vagon_delay.doroga       IS 'Дорога станции стоянки';
COMMENT ON COLUMN dpport.vagon_delay.date_from    IS 'Начало стоянки (time_op первой операции серии)';
COMMENT ON COLUMN dpport.vagon_delay.date_to      IS 'Конец эпизода; NULL — вагон стоит до сих пор';
COMMENT ON COLUMN dpport.vagon_delay.hours        IS 'Длительность эпизода в часах (проставляется при закрытии)';

-- Один открытый эпизод на вагон (гард целостности reconcile-слоя).
CREATE UNIQUE INDEX IF NOT EXISTS uq_vagon_delay_open
    ON dpport.vagon_delay (vagon) WHERE date_to IS NULL;

CREATE INDEX IF NOT EXISTS ix_vagon_delay_vagon     ON dpport.vagon_delay (vagon);
CREATE INDEX IF NOT EXISTS ix_vagon_delay_date_from ON dpport.vagon_delay (date_from);
CREATE INDEX IF NOT EXISTS ix_vagon_delay_group_key ON dpport.vagon_delay (group_key);
