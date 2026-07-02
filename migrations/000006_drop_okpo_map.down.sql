-- Откат 000006: вернуть okpo_map в config источника 'lk' (историческое значение).
SET search_path TO dpport;

UPDATE dpport.data_source
   SET config = jsonb_set(config, '{okpo_map}', '{"10230304":"AT","1126022":"NMTP"}'::jsonb),
       updated_at = now()
 WHERE id = 'lk';
