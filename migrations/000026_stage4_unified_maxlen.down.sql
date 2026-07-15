SET search_path TO dpport;

ALTER TABLE dpport.plan_profile DROP COLUMN IF EXISTS max_train_length;

ALTER TABLE dpport.plan_profile
    ADD COLUMN IF NOT EXISTS distribution_method text NOT NULL DEFAULT 'excel';

UPDATE dpport.plan_profile SET distribution_method = 'staircase' WHERE station_code = '984700';
