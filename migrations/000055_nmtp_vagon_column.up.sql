-- 000055: память колонки НМТП-отчёта по вагонам (решение владельца 30.07.2026).
--
--  Указание грузовладельца «считать этот поезд другой категорией» — факт про
--  ВАГОНЫ, а не про строку отчёта: привязка пишется поимённо по номерам состава
--  и при раскладке отчёта сильнее правил nmtp_column. Поезд может
--  переформироваться — каждый вагон всё равно предъявится в своей колонке.
--  Гашение: привязка удаляется сервисом, когда вагон выпал из подхода
--  (прибыл/выбыл), либо руками («вернуть по правилам» в модалке).
--  Прочие ручные правки отчёта НЕ хранятся (только экспорт с экрана).

SET search_path TO dpport;

CREATE TABLE IF NOT EXISTS dpport.nmtp_vagon_column (
    vagon      text PRIMARY KEY,
    column_id  bigint NOT NULL REFERENCES dpport.nmtp_column(id) ON DELETE CASCADE,
    created_at timestamp NOT NULL,
    created_by text NOT NULL DEFAULT ''
);

COMMENT ON TABLE  dpport.nmtp_vagon_column IS 'НМТП: ручная привязка вагона к колонке формы (сильнее правил nmtp_column)';
COMMENT ON COLUMN dpport.nmtp_vagon_column.vagon      IS 'Номер вагона';
COMMENT ON COLUMN dpport.nmtp_vagon_column.column_id  IS 'Колонка nmtp_column';
COMMENT ON COLUMN dpport.nmtp_vagon_column.created_at IS 'Когда назначена (МСК)';
COMMENT ON COLUMN dpport.nmtp_vagon_column.created_by IS 'Кто назначил';
