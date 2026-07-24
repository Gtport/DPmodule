-- ============================================================================
--  000046_bros_journal — журнал брошенных (подсистема «Брошенные», ветка 3).
--
--  Ежедневная фиксация состояния брошенного поезда оператором: код бросания,
--  комментарий, реквизиты заявки/письма. Одна запись в сутки на поезд
--  (UNIQUE bros_id+date, UPSERT), история накапливается — записи не теряются.
--
--  Особые коды (бизнес-правило РЖД, перенос gtport):
--    · 05 (платное размещение) — требует реквизиты заявки: zayavka_nomer/date,
--      date_pod (согласованная дата подъёма), reason_text;
--    · 01 (неприём грузополучателем) — требует is_agreed (согласованное с
--      гарантийным письмом / несогласованное = ответственность РЖД); согласованное
--      дополнительно требует реквизиты письма (zayavka_*, date_pod).
--
--  Время — Московское naive: date/заявка/подъём — date, created_at — timestamp
--  (через clock.Now()). plan_pod — план подвода на момент записи (из bros.plan).
--
--  Идемпотентно (CREATE TABLE IF NOT EXISTS), только добавляющая.
-- ============================================================================

SET search_path TO dpport;

CREATE TABLE IF NOT EXISTS dpport.bros_journal (
    id            bigserial PRIMARY KEY,
    bros_id       varchar(120) NOT NULL REFERENCES dpport.bros(id) ON DELETE CASCADE,
    date          date        NOT NULL DEFAULT CURRENT_DATE, -- сутки записи (уникальны с bros_id)
    reason        varchar(20) NOT NULL DEFAULT '',           -- код бросания
    comment       text        NOT NULL DEFAULT '',
    zayavka_nomer varchar(100),                              -- № заявки / гарантийного письма (коды 05/01-согл)
    zayavka_date  date,                                      -- дата заявки / письма
    date_pod      date,                                      -- согласованная дата подъёма
    reason_text   text,                                      -- расшифровка причины
    is_agreed     boolean,                                   -- код 01: согласованное (true) / несогласованное (false)
    plan_pod      date,                                      -- план подвода на момент записи
    created_at    timestamp   NOT NULL DEFAULT now(),
    created_by    varchar(100) NOT NULL DEFAULT '',          -- логин оператора ('system' — массовое сохранение)
    CONSTRAINT bros_journal_bros_date_key UNIQUE (bros_id, date)
);

CREATE INDEX IF NOT EXISTS idx_bros_journal_bros_id    ON dpport.bros_journal (bros_id);
CREATE INDEX IF NOT EXISTS idx_bros_journal_bros_recent ON dpport.bros_journal (bros_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_bros_journal_reason     ON dpport.bros_journal (reason);
