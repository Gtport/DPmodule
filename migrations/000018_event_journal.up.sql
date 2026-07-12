-- ============================================================================
--  000018_event_journal — единый журнал событий данных.
--
--  Одна таблица на два рода событий (сознательно единая, «туда же»):
--    * disl_update — снимок дислокации пересобран (приём ЛК, позже JSON АСУ);
--    * plan_upload — загружен план подвода (МА/НК).
--
--  Ключевое отличие doc_ts от created_at: doc_ts — время ИЗ документа (метка
--  формирования выгрузки ЛК / дата плана), created_at — когда факт записан на
--  сервере. Актуальность дислокации меряется по doc_ts (см. гард загрузки плана).
--  detail (jsonb) — разбивка по терминалам, имя файла, счётчики (для статус-панели).
--
--  Journal только ДОБАВЛЯЕТ записи (append-only), прежние не трогаются — в отличие
--  от снимка дислокации, который перезаписывается целиком (swap «вариант Б»). Именно
--  поэтому метку свежести храним здесь, а не в снимке: она переживает подмену.
--
--  Идемпотентно (CREATE TABLE IF NOT EXISTS). Время — МСК naive, без таймзоны.
-- ============================================================================

SET search_path TO dpport;

CREATE TABLE IF NOT EXISTS dpport.event_journal (
    id         bigserial PRIMARY KEY,
    event_type text NOT NULL,                                       -- disl_update | plan_upload
    source     text NOT NULL DEFAULT '',                            -- lk | json | plan_ma | plan_nk
    actor      text NOT NULL DEFAULT '',                            -- кто (username/email/subject из JWT)
    doc_ts     timestamp without time zone,                         -- время из документа (метка формирования ЛК / дата плана), МСК
    detail     jsonb NOT NULL DEFAULT '{}'::jsonb,                  -- разбивка по терминалам, имя файла, счётчики
    created_at timestamp without time zone NOT NULL DEFAULT now()   -- когда записано (МСК, ставим из clock.Now())
);

-- Выборки журнала: последние события по типу (для гарда актуальности и панели).
CREATE INDEX IF NOT EXISTS ix_event_journal_type_created
    ON dpport.event_journal (event_type, created_at DESC);
