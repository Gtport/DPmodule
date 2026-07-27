-- ============================================================================
--  000048_vagon_delay_terminal — терминал (gruzpol_s) в эпизодах задержек.
--
--  Требование владельца: в «Задержанных вагонах» терминал обязателен, отчёт
--  по простоям фильтруется по терминалу (по аналогии с отчётом «Брошенных»).
--  Колонка заполняется reconcile-слоем (applyDelays) из снимка; существующие
--  эпизоды бэкфиллятся из vagon_history по рейсу (вагон + дата начала).
--
--  Идемпотентно (ADD COLUMN IF NOT EXISTS), только добавляющая.
-- ============================================================================

SET search_path TO dpport;

ALTER TABLE dpport.vagon_delay
    ADD COLUMN IF NOT EXISTS gruzpol_s varchar(50) NOT NULL DEFAULT '';

COMMENT ON COLUMN dpport.vagon_delay.gruzpol_s IS 'Терминал (краткое имя причала) — куда едет вагон; из снимка дислокации';

-- Бэкфилл существующих эпизодов из строки рейса.
UPDATE dpport.vagon_delay d
SET gruzpol_s = h.gruzpol_s
FROM dpport.vagon_history h
WHERE d.gruzpol_s = '' AND h.gruzpol_s <> ''
  AND h.vagon = d.vagon AND h.date_nach_d = d.date_nach_d;

CREATE INDEX IF NOT EXISTS ix_vagon_delay_gruzpol ON dpport.vagon_delay (gruzpol_s);
