SET search_path TO dpport;

ALTER TABLE dpport.plan_profile DROP COLUMN IF EXISTS distribution_method;
