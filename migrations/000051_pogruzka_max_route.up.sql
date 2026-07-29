-- 000051: маршруты MAX формы «Погрузка» (отчёт страницы «Справки и отчёты»).
--
--  Зеркало рассылки gtport LoadingReport (MAX_CHATS_CONFIG из кода → данные):
--  сводка по всем терминалам → оперативный чат, срез терминала → его чат.
--  Guard WHERE EXISTS: сид только для реально заведённых чатов.

SET search_path TO dpport;

INSERT INTO dpport.max_route (report, terminal, chat_name, sort_order, enabled)
SELECT 'pogruzka', t.terminal, t.chat, t.so, true
FROM (VALUES ('АЭ', 'at', 10), ('ГУТ-2', 'gut', 20), ('УТ-1', 'ut', 30), ('', 'oper', 40))
     AS t(terminal, chat, so)
WHERE EXISTS (SELECT 1 FROM dpport.max_chat c WHERE c.name = t.chat)
ON CONFLICT ON CONSTRAINT max_route_key DO NOTHING;
