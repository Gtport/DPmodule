SET search_path TO dpport;

DELETE FROM dpport.list_tables WHERE name IN ('max_chat', 'max_route');
DROP TABLE IF EXISTS dpport.max_route;
DROP TABLE IF EXISTS dpport.max_chat;
