SET search_path TO dpport;

DELETE FROM dpport.max_route WHERE report = 'podhod';
DELETE FROM dpport.list_tables WHERE name = 'report_preset';
DROP TABLE IF EXISTS dpport.report_preset;
