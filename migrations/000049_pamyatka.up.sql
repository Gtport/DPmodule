-- ============================================================================
--  000049_pamyatka — заполнение вех подачи/уборки в vagon_history из памяток
--  ГУ-45 внешнего провайдера (крон-инкремент <client>/reference/update).
--
--  Колонки под памятки в vagon_history существуют с 000001 (перенесены из схемы
--  gtport), но никогда не заполнялись: в gtport эти данные вбивали руками
--  Excel-файлом «Журнал движения». Здесь их начинает вести крон.
--
--  РАСКЛАДКА (решение владельца 28.07.2026 — в модели gtport стояло «уточнить»):
--    памятка «на подачу»  → nom_gu45_pod  ← NUMBER_PAMYATKA
--                           date_gu45_pod ← DATE_CREATE (дата самой памятки)
--                           date_pod      ← GET_IN      (фактическая подача)
--                           place_pod     ← GET_PLACE   (фронт подачи)
--    памятка «на уборку»  → nom_gu45_ubor  ← NUMBER_PAMYATKA
--                           date_gu45_ubor ← DATE_CREATE
--                           date_ubor      ← GET_OUT    (фактическая уборка)
--    в обеих             → date_vigr_gu45 ← REPORT (уведомление об окончании
--                           грузовой операции = факт выгрузки по ГУ-45; второй
--                           источник рядом с вехой date_vigr из истории АСУ,
--                           её НЕ перетирает — расхождение остаётся видимым).
--    date_pod_gu45 / date_ubor_gu45 остаются пустыми: это колонки-дубли, факт
--    подачи/уборки живёт в date_pod/date_ubor.
--
--  pamyatka_state — стадия заполнения рейса памятками (решение владельца):
--    0 — памяток не было (дефолт), 1 — подача проставлена, 2 — уборка проставлена.
--  Смысл — сузить матч: памятка на подачу ищет рейсы со стадией 0, на уборку —
--  0 или 1. Формально признак дублирует непустой nom_gu45_pod/nom_gu45_ubor,
--  но отдельным числом выборка кандидатов дешевле и правило читается глазами.
--
--  Время — Московское naive (жёсткий инвариант): timestamp without time zone.
--
--  Идемпотентно (IF NOT EXISTS), только добавляющая: существующие колонки и
--  данные не трогает, у 12 054 накопленных рейсов стадия станет 0 = «памяток
--  не было», что и есть правда.
-- ============================================================================

SET search_path TO dpport;

ALTER TABLE dpport.vagon_history
    ADD COLUMN IF NOT EXISTS pamyatka_state smallint NOT NULL DEFAULT 0;

COMMENT ON COLUMN dpport.vagon_history.pamyatka_state IS
    'Стадия заполнения памятками ГУ-45: 0 — не было, 1 — подача, 2 — уборка';
COMMENT ON COLUMN dpport.vagon_history.nom_gu45_pod   IS 'Номер памятки на подачу (NUMBER_PAMYATKA)';
COMMENT ON COLUMN dpport.vagon_history.date_gu45_pod  IS 'Дата составления памятки на подачу (DATE_CREATE)';
COMMENT ON COLUMN dpport.vagon_history.date_pod       IS 'Фактическая подача по памятке (GET_IN)';
COMMENT ON COLUMN dpport.vagon_history.place_pod      IS 'Фронт подачи по памятке (GET_PLACE)';
COMMENT ON COLUMN dpport.vagon_history.nom_gu45_ubor  IS 'Номер памятки на уборку (NUMBER_PAMYATKA)';
COMMENT ON COLUMN dpport.vagon_history.date_gu45_ubor IS 'Дата составления памятки на уборку (DATE_CREATE)';
COMMENT ON COLUMN dpport.vagon_history.date_ubor      IS 'Фактическая уборка по памятке (GET_OUT)';
COMMENT ON COLUMN dpport.vagon_history.date_vigr_gu45 IS 'Окончание грузовой операции по памятке (REPORT)';

-- Курсор инкремента: провайдер отдаёт своё значение LAST_UPDATE в каждом ответе,
-- следующий запрос уходит ровно с ним. Держать курсор в памяти нельзя — рестарт
-- бэкенда потерял бы позицию, а «сейчас − interval» пропускает пачки при любом
-- пропущенном тике. Одна строка на клиента провайдера (attis/nmtp).
CREATE TABLE IF NOT EXISTS dpport.pamyatka_cursor (
    client      varchar(50)  PRIMARY KEY,          -- код клиента провайдера (путь запроса)
    last_update varchar(40)  NOT NULL DEFAULT '',  -- курсор ДОСЛОВНО как пришёл ("2026-07-27 22:22:51.869")
    updated_at  timestamp    NOT NULL DEFAULT now()
);

COMMENT ON TABLE  dpport.pamyatka_cursor             IS 'Курсор инкремента памяток ГУ-45 по клиентам провайдера';
COMMENT ON COLUMN dpport.pamyatka_cursor.last_update IS 'Значение LAST_UPDATE последнего непустого ответа провайдера (дословно)';
