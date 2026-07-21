-- 601 «История продвижения вагона»: маппинг клиента провайдера + очередь запросов.

-- Клиент провайдера АСУ для запросов по вагонам грузополучателя (по ОКПО).
-- Данные (attis/nmtp) проставляет владелец UPDATE-ом — не хардкодим в миграции.
ALTER TABLE dpport.ports ADD COLUMN IF NOT EXISTS provider_client varchar(32) NOT NULL DEFAULT '';
COMMENT ON COLUMN dpport.ports.provider_client IS 'Клиент провайдера АСУ (attis/nmtp) для запроса истории продвижения (601): вагон в базе РЖД строго за грузополучателем';

-- Очередь запросов 601. PK trip_key: повторный триггер по тому же рейсу обновляет
-- заявку (дедупликация групповых смен статусов). Разгребает фоновый воркер.
CREATE TABLE IF NOT EXISTS dpport.vagon_op_request (
    trip_key    bigint      PRIMARY KEY,
    vagon       varchar(8)  NOT NULL,
    date_nach_d date        NOT NULL,
    client      varchar(32) NOT NULL,
    reason      varchar(32) NOT NULL,
    priority    int         NOT NULL DEFAULT 0,
    attempts    int         NOT NULL DEFAULT 0,
    last_error  text        NOT NULL DEFAULT '',
    created_at  timestamp   NOT NULL,
    updated_at  timestamp   NOT NULL
);
COMMENT ON TABLE dpport.vagon_op_request IS 'Очередь запросов 601 «История продвижения вагона»: триггеры конвейера (прибытие/пропажа/выбытие-10) и ручные; воркер разгребает пачками с паузой';
COMMENT ON COLUMN dpport.vagon_op_request.reason IS 'Причина: arrival / missing / departed / manual';

CREATE INDEX IF NOT EXISTS idx_vagon_op_request_order ON dpport.vagon_op_request (priority DESC, created_at);
