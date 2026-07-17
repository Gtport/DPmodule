DELETE FROM dpport.list_tables WHERE name IN ('sf', 'naznach_station', 'cargo');

COMMENT ON COLUMN dpport.sf.id       IS NULL;
COMMENT ON COLUMN dpport.sf.sinonim  IS NULL;
COMMENT ON COLUMN dpport.sf.station  IS NULL;
COMMENT ON COLUMN dpport.sf.quantity IS NULL;
COMMENT ON COLUMN dpport.sf.enabled  IS NULL;

COMMENT ON COLUMN dpport.naznach_station.id             IS NULL;
COMMENT ON COLUMN dpport.naznach_station.dest_station   IS NULL;
COMMENT ON COLUMN dpport.naznach_station.origin_station IS NULL;
COMMENT ON COLUMN dpport.naznach_station.naznach        IS NULL;
COMMENT ON COLUMN dpport.naznach_station.univers        IS NULL;
COMMENT ON COLUMN dpport.naznach_station.enabled        IS NULL;

COMMENT ON COLUMN dpport.cargo.cargo_kod   IS NULL;
COMMENT ON COLUMN dpport.cargo.name        IS NULL;
COMMENT ON COLUMN dpport.cargo.cargo_group IS NULL;
COMMENT ON COLUMN dpport.cargo.cargo_s     IS NULL;
COMMENT ON COLUMN dpport.cargo.cargo_sms   IS NULL;
