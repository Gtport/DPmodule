SET search_path TO dpport;

ALTER TABLE dpport.gt_forecast_snapshot DROP COLUMN IF EXISTS meta;
ALTER TABLE dpport.gt_forecast_snapshot DROP COLUMN IF EXISTS kind;
ALTER TABLE dpport.gt_forecast_snapshot DROP COLUMN IF EXISTS computed_at;
