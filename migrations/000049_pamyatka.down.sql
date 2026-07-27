-- Откат 000049_pamyatka. Колонку pamyatka_state снимаем вместе с курсором:
-- без движка памяток она смысла не имеет. Заполненные вехи подачи/уборки
-- (date_pod/date_ubor/…) НЕ трогаем — это данные рейса, а не служебное поле.

SET search_path TO dpport;

DROP TABLE IF EXISTS dpport.pamyatka_cursor;

ALTER TABLE dpport.vagon_history
    DROP COLUMN IF EXISTS pamyatka_state;
