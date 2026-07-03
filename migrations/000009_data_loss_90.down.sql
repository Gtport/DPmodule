-- Откат 000009: вернуть порог потери данных к 30%.
SET search_path TO dpport;

UPDATE dpport.client_settings
   SET ingest_policy = jsonb_set(ingest_policy, '{dislocation,max_data_loss_pct}', '30'),
       updated_at = now()
 WHERE id = 1;
