-- ============================================================================
--  000005_init_route_speed — скоростной профиль до станции назначения (§3.12).
--
--  Заменяет захардкоженные скорости из gtlogic enrich_stage2.go (switch по
--  StationNach: УЛАК/ЧЕГДОМЫН/default, пороги остатка 1364/911 км). Время хода по
--  участку = расстояние_участка / speed; участок выбирается по остатку расстояния
--  до назначения (строка с наибольшим from_km ≤ остаток).
--
--  Ключ профиля — (station_nach, is_bam). station_nach = '*' — профиль по
--  умолчанию (data-driven аналог ветки default). is_bam — альтернативный маршрут
--  (БАМ) со своими скоростями (новый параметр; профили БАМ сидируются позже).
--
--  Значения — через seed (scripts/seed_directories.sql ← _reference/seed/
--  route_speed.csv), НЕ хардкодом в миграции: маршруты специфичны под клиента.
-- ============================================================================

SET search_path TO dpport;

CREATE TABLE dpport.route_speed (
    id           bigserial PRIMARY KEY,
    station_nach text    NOT NULL,               -- станция отправления; '*' = по умолчанию
    is_bam       boolean NOT NULL DEFAULT false, -- альтернативный маршрут (БАМ)
    from_km      integer NOT NULL,               -- нижняя граница участка (км ДО назначения)
    speed        numeric NOT NULL,               -- км/ч на участке
    UNIQUE (station_nach, is_bam, from_km)
);
