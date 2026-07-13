SET search_path TO dpport;

DELETE FROM dpport.data_source WHERE id = 'asu';

UPDATE dpport.client_settings
   SET ingest_policy = ingest_policy #- '{dislocation,max_source_skew_minutes}',
       updated_at = now()
 WHERE id = 1;
