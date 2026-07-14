SET search_path TO dpport;

UPDATE dpport.data_source
   SET config = config #- '{auth_header}'
 WHERE id = 'asu';
