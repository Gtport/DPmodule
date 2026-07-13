-- ============================================================================
--  000021_asu_source — источник автозагрузки дислокации из АСУ-АСУ (api_pull).
--
--  Провайдер отдаёт снимок по маршруту <base_url>/<client>/dislocation в формате
--  {timestamp,count,wagons} (envelope, parser.JSONParser). Один источник перечисляет
--  всех своих клиентов (attis/nmtp); ingest тянет их за один проход, сверяет метки
--  формирования и пересобирает снимок общим конвейером (полный снимок, как ЛК).
--
--  Строка заведена ВЫКЛЮЧЕННОЙ и с пустым base_url: реальный URL/домен и секрет не
--  коммитим (инфра-правило). Владелец заполняет base_url, кладёт токен в env
--  (ASU_TOKEN) и включает источник:
--    UPDATE dpport.data_source
--       SET config = jsonb_set(config,'{base_url}','"https://<хост-АСУ>"'), enabled = true
--     WHERE id='asu';
--
--  Порог рассогласования меток формирования между клиентами — max_source_skew_minutes
--  (мин); 0/отсутствие → гард выключен. Кладём под ingest_policy.dislocation.
-- ============================================================================

SET search_path TO dpport;

INSERT INTO dpport.data_source (id, name, enabled, ingest, category, config, sort_order) VALUES
 ('asu', 'Дислокация из АСУ-АСУ (авто)', false, 'api_pull', 'dislocation',
   '{"base_url":"",
     "clients":["attis","nmtp"],
     "path_template":"/{client}/dislocation",
     "method":"GET",
     "auth_secret_key":"ASU_TOKEN",
     "timeout_secs":30}',
   10)
ON CONFLICT (id) DO NOTHING;

UPDATE dpport.client_settings
   SET ingest_policy = jsonb_set(ingest_policy, '{dislocation,max_source_skew_minutes}', '2'),
       updated_at = now()
 WHERE id = 1;
