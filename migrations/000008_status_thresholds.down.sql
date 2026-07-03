-- Откат 000008: убрать пороги статусов из extra.
SET search_path TO dpport;

UPDATE dpport.client_settings
   SET extra = extra - 'status', updated_at = now()
 WHERE id = 1;
