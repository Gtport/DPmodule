SET search_path TO dpport;

UPDATE dpport.client_settings
   SET ingest_policy = ingest_policy #- '{plan,plan_max_disl_age_minutes}',
       updated_at = now()
 WHERE id = 1;
