-- 000052: маршруты MAX формы «Выгрузка за день» (скрин-форма страницы
-- «Справки и отчёты», перенос gtport CargoReport).
--
--  В gtport дневная выгрузка слалась в чат своего порта (MAX_CHATS_CONFIG:
--  at→at, ut→ut, gut→gut) — здесь то же самое данными. Guard WHERE EXISTS:
--  сид только для реально заведённых чатов.

SET search_path TO dpport;

INSERT INTO dpport.max_route (report, terminal, chat_name, sort_order, enabled)
SELECT 'vygruzka', t.terminal, t.chat, t.so, true
FROM (VALUES ('АЭ', 'at', 10), ('ГУТ-2', 'gut', 20), ('УТ-1', 'ut', 30))
     AS t(terminal, chat, so)
WHERE EXISTS (SELECT 1 FROM dpport.max_chat c WHERE c.name = t.chat)
ON CONFLICT ON CONSTRAINT max_route_key DO NOTHING;
