SET search_path TO dpport;

UPDATE dpport.client_settings SET extra = extra #- '{stage4}' WHERE id = 1;
ALTER TABLE dpport.plan_profile DROP COLUMN IF EXISTS slot_tolerance_h;
DELETE FROM dpport.nitka_schedule WHERE station_code = '984700';
