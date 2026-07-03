-- ============================================================================
--  000008_status_thresholds — пороги простоя для статуса 4 (§3.13).
--
--  Общепрограмные пороги расчёта статуса дислокации выносятся из кода в
--  client_settings.extra.status: prost_dn_min (сутки), prost_ch_min (часы).
--  Значения GTport: 1 сутки / 12 часов. Идемпотентно (merge по ключу status).
-- ============================================================================

SET search_path TO dpport;

UPDATE dpport.client_settings
   SET extra = extra || '{"status":{"prost_dn_min":1,"prost_ch_min":12}}'::jsonb,
       updated_at = now()
 WHERE id = 1;
