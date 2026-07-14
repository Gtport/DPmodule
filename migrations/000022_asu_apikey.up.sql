-- ============================================================================
--  000022_asu_apikey — провайдер АСУ (attis/nmtp) авторизует по заголовку
--  X-API-Key, а не Bearer. Прописываем схему авторизации источнику 'asu'.
--
--  auth_header — имя заголовка, в который HTTP-клиент кладёт секрет (значение из
--  env по auth_secret_key=ASU_TOKEN). Пустой auth_header означал бы старое
--  поведение "Authorization: Bearer <секрет>".
--
--  НЕ секреты и НЕ инфра — можно коммитить. Реальный домен/IP и сам ключ по-прежнему
--  не коммитим: владелец заполняет base_url и (для самоподписанного серта на IP)
--  включает insecure_tls, кладёт ключ в env ASU_TOKEN и включает источник:
--    UPDATE dpport.data_source
--       SET config = config
--             || jsonb_build_object('base_url','https://<хост-АСУ>:8443/api/v1')
--             || jsonb_build_object('insecure_tls', true),   -- эквивалент curl -k
--           enabled = true
--     WHERE id='asu';
--    # env процесса backend:  ASU_TOKEN=<значение X-API-Key>
-- ============================================================================

SET search_path TO dpport;

UPDATE dpport.data_source
   SET config = jsonb_set(config, '{auth_header}', '"X-API-Key"')
 WHERE id = 'asu';
