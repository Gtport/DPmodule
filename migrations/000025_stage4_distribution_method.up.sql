-- ============================================================================
--  000025_stage4_distribution_method — метод раскладки поездов по ниткам (Stage 4).
--
--  У эталона GTport ДВА алгоритма распределения беспланных поездов по слотам:
--    • «staircase» (distributeGroupWithInterval) — последовательная лестница:
--      currentTime ре-якорится на НАЗНАЧЕННУЮ нитку, следующий поезд ≥ currentTime +
--      интервал; допуск −6ч применяется ТОЛЬКО к Rasch. Так работает УТ-1 (984700).
--    • «excel» (distributeAEGUT2ByExcelMethod) — общий пул + глобальная сортировка по
--      Rasch + поиск ближайшей свободной нитки. Так работают АЭ/ГУТ-2 (985702).
--
--  Раньше мы применяли ко ВСЕМ станциям excel-метод → УТ-1 систематически «раньше»
--  gtport (первый поезд мог встать до стартовой нитки, допуск съедал интервальный пол).
--  Метод делаем настройкой профиля станции, без хардкода: 984700 → staircase, прочим
--  excel по умолчанию.
-- ============================================================================

SET search_path TO dpport;

ALTER TABLE dpport.plan_profile
    ADD COLUMN IF NOT EXISTS distribution_method text NOT NULL DEFAULT 'excel';

UPDATE dpport.plan_profile SET distribution_method = 'staircase' WHERE station_code = '984700';
