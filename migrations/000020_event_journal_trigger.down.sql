SET search_path TO dpport;

ALTER TABLE dpport.event_journal DROP COLUMN IF EXISTS trigger;
