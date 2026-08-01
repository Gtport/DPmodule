SET search_path TO dpport;

DELETE FROM dpport.list_tables WHERE name = 'lk_account';
DROP TABLE IF EXISTS dpport.lk_account;
