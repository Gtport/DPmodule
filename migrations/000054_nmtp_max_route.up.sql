-- 000054: маршруты MAX формы «Отчёты НМТП» («Подход вагонов» по форме порта).
--
--  В gtport PortReportNmtp слался в чат своего терминала (MAX_CHATS_CONFIG:
--  АЭ→at, ГУТ-2→gut, УТ-1→ut) — здесь то же самое данными, как у vygruzka
--  (000052). Guard WHERE EXISTS: сид только для реально заведённых чатов.

SET search_path TO dpport;

INSERT INTO dpport.max_route (report, terminal, chat_name, sort_order, enabled)
SELECT 'nmtp', t.terminal, t.chat, t.so, true
FROM (VALUES ('АЭ', 'at', 10), ('ГУТ-2', 'gut', 20), ('УТ-1', 'ut', 30))
     AS t(terminal, chat, so)
WHERE EXISTS (SELECT 1 FROM dpport.max_chat c WHERE c.name = t.chat)
ON CONFLICT ON CONSTRAINT max_route_key DO NOTHING;
