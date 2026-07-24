-- ============================================================================
--  000045_bros — снимок брошенных поездов (подсистема «Брошенные», ветка 2).
--
--  Одна строка = один брошенный поезд (агрегат вагонов статуса 5, code_oper=92),
--  ключ `id` = id_status5 (`index|code_station_oper|time_op`, brosKey из Stage 1).
--  Жизненный цикл: status_br=true (активен) → false (поднят). Заполняется
--  reconcile-слоем конвейера (`applyBros`) после Stage 4: свежий батч сверяется
--  с таблицей — новые броски INSERT, продолжающиеся UPDATE (состав/план/индекс),
--  поднятые (вагоны ушли из статуса 5) STOP (status_br=false, date_pod_fact).
--
--  Уход от gtport: там снимок бросков вёлся очередью new/stop при инкрементальной
--  сборке; здесь снимок пересобирается целиком, поэтому reconcile против таблицы
--  (как status9/vagon_history) — тот же результат без stateful-очереди.
--
--  Время — Московское naive (жёсткий инвариант): даты подъёма/броса/плана — date,
--  прогнозы и штампы — timestamp without time zone (через clock.Now()).
--
--  Поля reason/comment/date_pod (код бросания, комментарий, план подъёма) правит
--  оператор в журнале — ветка 3; здесь колонки заведены, но авто-слоем не пишутся.
--
--  Идемпотентно (CREATE TABLE IF NOT EXISTS), только добавляющая.
-- ============================================================================

SET search_path TO dpport;

CREATE TABLE IF NOT EXISTS dpport.bros (
    id            varchar(120) PRIMARY KEY,        -- = id_status5 (index|code_station_oper|time_op)
    id_index      varchar(50)  NOT NULL DEFAULT '',
    index_0       varchar(50)  NOT NULL DEFAULT '', -- индекс на момент бросания
    index_1       varchar(50)  NOT NULL DEFAULT '', -- текущий/на момент подъёма
    station_br    varchar(120) NOT NULL DEFAULT '', -- станция бросания
    doroga_br     varchar(50)  NOT NULL DEFAULT '',
    date_br       date,                             -- дата бросания
    gruzpol_s     varchar(50)  NOT NULL DEFAULT '', -- терминал (краткое имя причала)
    date_pod      date,                             -- плановая дата подъёма (оператор, ветка 3)
    date_pod_fact date,                             -- фактическая дата подъёма (STOP)
    prog_0        timestamp,                        -- прогноз прибытия на момент бросания
    prog_1        timestamp,                        -- прогноз на момент подъёма
    to_go         numeric(6,2),                     -- расчётное время хода, часы
    plan          date,                             -- план подвода (JD)
    plan_history  text        NOT NULL DEFAULT '',  -- история изменений плана (через ", ")
    status_br     boolean     NOT NULL DEFAULT false,
    reason        varchar(20) NOT NULL DEFAULT '',  -- код бросания (журнал, ветка 3)
    comment       text        NOT NULL DEFAULT '',
    sostav        text        NOT NULL DEFAULT '',  -- описание состава (станция груз (N) на назначение)
    vagon_count   integer     NOT NULL DEFAULT 0,
    created_at    timestamp   NOT NULL DEFAULT now(),
    updated_at    timestamp   NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_bros_status_br  ON dpport.bros (status_br);
CREATE INDEX IF NOT EXISTS idx_bros_date_br    ON dpport.bros (date_br);
CREATE INDEX IF NOT EXISTS idx_bros_gruzpol_s  ON dpport.bros (gruzpol_s);
CREATE INDEX IF NOT EXISTS idx_bros_index_0    ON dpport.bros (index_0);
CREATE INDEX IF NOT EXISTS idx_bros_updated_at ON dpport.bros (updated_at);
