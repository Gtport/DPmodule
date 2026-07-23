-- ============================================================================
--  import_vigr.sql — разовый перенос учётных листов выгрузки/погрузки из gtport
--  (таблицы vigr_at/vigr_ut/vigr_gut) в универсальную dpport.cargo_work[_load].
--
--  Соответствие:
--    vigr_at  → cargo_work terminal='АЭ',   cargo_key='' (одна линия)
--    vigr_ut  → cargo_work terminal='УТ-1', cargo_key='' + погрузка → cargo_work_load
--    vigr_gut → cargo_work terminal='ГУТ-2' × {УГОЛЬ,МЕТАЛЛ,ЧУГУН} + погрузка
--  Погрузка (glin/pek/ruda/kont/proch) → cargo_work_load (GLIN/PEK/RUDA/KONT/PROCH).
--  date gtport = ЖД-сутки → date_jd. gtport — источник истории: ON CONFLICT DO UPDATE.
--
--  CSV лежат в /home/alex/projects/files/ (вне git). Запуск из корня репозитория:
--    psql "$PG_DSN" -v ON_ERROR_STOP=1 -f scripts/import_vigr.sql
--  Идемпотентно (ON CONFLICT по естественному ключу). Временные таблицы — all-text
--  (устойчиво к парсингу), приведение типов — в INSERT; 'NULL' → SQL NULL.
-- ============================================================================

SET search_path TO dpport;

-- ── Стандартные терминалы (АЭ, УТ-1): одна линия выгрузки + погрузка ─────────
CREATE TEMP TABLE t_std (
  date text, ost_18 text, ost_st text, prib text, plan text, vigr_fact text, vigr_stan text,
  ost text, useful_formation text, total_formation text, downtime text,
  analytics_json text, train_structure_json text, prim text, effectiv text, perepokaz text,
  created_at text, updated_at text,
  glin_load text, glin_plan text, glin_ost text, pek_load text, pek_plan text, pek_ost text,
  ruda_load text, ruda_plan text, ruda_ost text, kont_load text, kont_plan text, kont_ost text,
  proch_load text, proch_plan text, proch_ost text
) ON COMMIT DROP;

-- Перенос одного стандартного терминала: заполняется t_std до вызова.
CREATE OR REPLACE FUNCTION pg_temp.load_std(term text) RETURNS void AS $$
BEGIN
  INSERT INTO dpport.cargo_work (date_jd, terminal, cargo_key, ost_18, ost_st, prib, vigr_stan,
    useful_formation, total_formation, downtime, plan, vigr_fact, prim, ost, effectiv, perepokaz,
    analytics_json, train_structure_json, created_at, updated_at)
  SELECT date::date, term, '', ost_18::int, ost_st::int, prib::int, vigr_stan::int,
    useful_formation::int, total_formation::int, coalesce(downtime,''), plan::int, vigr_fact::int,
    coalesce(prim,''), ost::int, effectiv::int, perepokaz::int,
    analytics_json::jsonb, train_structure_json::jsonb, created_at::timestamp, updated_at::timestamp
  FROM t_std
  ON CONFLICT (date_jd, terminal, cargo_key) DO UPDATE SET
    ost_18=excluded.ost_18, ost_st=excluded.ost_st, prib=excluded.prib, vigr_stan=excluded.vigr_stan,
    useful_formation=excluded.useful_formation, total_formation=excluded.total_formation,
    downtime=excluded.downtime, plan=excluded.plan, vigr_fact=excluded.vigr_fact, prim=excluded.prim,
    ost=excluded.ost, effectiv=excluded.effectiv, perepokaz=excluded.perepokaz,
    analytics_json=excluded.analytics_json, train_structure_json=excluded.train_structure_json,
    updated_at=excluded.updated_at;

  INSERT INTO dpport.cargo_work_load (date_jd, terminal, cargo_key, load_fact, plan, ost, created_at, updated_at)
  SELECT t.date::date, term, v.key, v.load::int, v.plan::int, v.ost::int,
         t.created_at::timestamp, t.updated_at::timestamp
  FROM t_std t CROSS JOIN LATERAL (VALUES
    ('GLIN', t.glin_load, t.glin_plan, t.glin_ost),
    ('PEK',  t.pek_load,  t.pek_plan,  t.pek_ost),
    ('RUDA', t.ruda_load, t.ruda_plan, t.ruda_ost),
    ('KONT', t.kont_load, t.kont_plan, t.kont_ost),
    ('PROCH',t.proch_load,t.proch_plan,t.proch_ost)
  ) AS v(key, load, plan, ost)
  -- только линии погрузки, заведённые у терминала (у АЭ их нет)
  WHERE EXISTS (SELECT 1 FROM dpport.port_cargo_line l
                 WHERE l.terminal=term AND l.kind='load' AND l.cargo_key=v.key)
  ON CONFLICT (date_jd, terminal, cargo_key) DO UPDATE SET
    load_fact=excluded.load_fact, plan=excluded.plan, ost=excluded.ost, updated_at=excluded.updated_at;
END;
$$ LANGUAGE plpgsql;

\copy t_std FROM '/home/alex/projects/files/vigr_at.csv' WITH (FORMAT csv, HEADER true, NULL 'NULL')
SELECT pg_temp.load_std('АЭ');
TRUNCATE t_std;
\copy t_std FROM '/home/alex/projects/files/vigr_ut.csv' WITH (FORMAT csv, HEADER true, NULL 'NULL')
SELECT pg_temp.load_std('УТ-1');

-- ── ГУТ-2: три линии выгрузки (уголь/металл/чугун) + погрузка ────────────────
CREATE TEMP TABLE t_gut (
  date text,
  coal_ost_18 text, coal_y_ost_st text, coal_prib text, coal_plan text, coal_vigr_fact text,
  coal_vigr_stan text, coal_ost text, coal_useful_formation text, coal_total_formation text,
  coal_downtime text, coal_analytics_json text, coal_train_structure_json text, coal_effectiv text, coal_perepokaz text,
  metal_ost_18 text, metal_ost_st text, metal_prib text, metal_plan text, metal_vigr_fact text,
  metal_vigr_stan text, metal_ost text, metal_useful_formation text, metal_total_formation text,
  metal_downtime text, metal_analytics_json text, metal_train_structure_json text, metal_effectiv text, metal_perepokaz text,
  chugun_ost_18 text, chugun_ost_st text, chugun_prib text, chugun_plan text, chugun_vigr_fact text,
  chugun_vigr_stan text, chugun_ost text, chugun_useful_formation text, chugun_total_formation text,
  chugun_downtime text, chugun_analytics_json text, chugun_train_structure_json text, chugun_effectiv text, chugun_perepokaz text,
  train_structure_json text, prim text, created_at text, updated_at text,
  glin_load text, glin_plan text, glin_ost text, pek_load text, pek_plan text, pek_ost text,
  ruda_load text, ruda_plan text, ruda_ost text, kont_load text, kont_plan text, kont_ost text,
  proch_load text, proch_plan text, proch_ost text
) ON COMMIT DROP;

\copy t_gut FROM '/home/alex/projects/files/vigr_gut.csv' WITH (FORMAT csv, HEADER true, NULL 'NULL')

INSERT INTO dpport.cargo_work (date_jd, terminal, cargo_key, ost_18, ost_st, prib, vigr_stan,
  useful_formation, total_formation, downtime, plan, vigr_fact, prim, ost, effectiv, perepokaz,
  analytics_json, train_structure_json, created_at, updated_at)
SELECT t.date::date, 'ГУТ-2', v.key, v.ost18::int, v.ost_st::int, v.prib::int, v.vigr_stan::int,
  v.useful::int, v.total::int, coalesce(v.downtime,''), v.plan::int, v.vigr_fact::int, coalesce(t.prim,''),
  v.ost::int, v.effectiv::int, v.perepokaz::int, v.analytics::jsonb, v.tstruct::jsonb,
  t.created_at::timestamp, t.updated_at::timestamp
FROM t_gut t CROSS JOIN LATERAL (VALUES
  ('УГОЛЬ', coal_ost_18, coal_y_ost_st, coal_prib, coal_plan, coal_vigr_fact, coal_vigr_stan, coal_ost,
           coal_useful_formation, coal_total_formation, coal_downtime, coal_effectiv, coal_perepokaz,
           coal_analytics_json, coal_train_structure_json),
  ('МЕТАЛЛ', metal_ost_18, metal_ost_st, metal_prib, metal_plan, metal_vigr_fact, metal_vigr_stan, metal_ost,
           metal_useful_formation, metal_total_formation, metal_downtime, metal_effectiv, metal_perepokaz,
           metal_analytics_json, metal_train_structure_json),
  ('ЧУГУН', chugun_ost_18, chugun_ost_st, chugun_prib, chugun_plan, chugun_vigr_fact, chugun_vigr_stan, chugun_ost,
           chugun_useful_formation, chugun_total_formation, chugun_downtime, chugun_effectiv, chugun_perepokaz,
           chugun_analytics_json, chugun_train_structure_json)
) AS v(key, ost18, ost_st, prib, plan, vigr_fact, vigr_stan, ost, useful, total, downtime, effectiv, perepokaz, analytics, tstruct)
ON CONFLICT (date_jd, terminal, cargo_key) DO UPDATE SET
  ost_18=excluded.ost_18, ost_st=excluded.ost_st, prib=excluded.prib, vigr_stan=excluded.vigr_stan,
  useful_formation=excluded.useful_formation, total_formation=excluded.total_formation,
  downtime=excluded.downtime, plan=excluded.plan, vigr_fact=excluded.vigr_fact, prim=excluded.prim,
  ost=excluded.ost, effectiv=excluded.effectiv, perepokaz=excluded.perepokaz,
  analytics_json=excluded.analytics_json, train_structure_json=excluded.train_structure_json,
  updated_at=excluded.updated_at;

INSERT INTO dpport.cargo_work_load (date_jd, terminal, cargo_key, load_fact, plan, ost, created_at, updated_at)
SELECT t.date::date, 'ГУТ-2', v.key, v.load::int, v.plan::int, v.ost::int,
       t.created_at::timestamp, t.updated_at::timestamp
FROM t_gut t CROSS JOIN LATERAL (VALUES
  ('GLIN', t.glin_load, t.glin_plan, t.glin_ost),
  ('PEK',  t.pek_load,  t.pek_plan,  t.pek_ost),
  ('RUDA', t.ruda_load, t.ruda_plan, t.ruda_ost),
  ('KONT', t.kont_load, t.kont_plan, t.kont_ost),
  ('PROCH',t.proch_load,t.proch_plan,t.proch_ost)
) AS v(key, load, plan, ost)
ON CONFLICT (date_jd, terminal, cargo_key) DO UPDATE SET
  load_fact=excluded.load_fact, plan=excluded.plan, ost=excluded.ost, updated_at=excluded.updated_at;

-- Контроль:
SELECT terminal, cargo_key, count(*) FROM dpport.cargo_work GROUP BY 1,2 ORDER BY 1,2;
SELECT terminal, count(*) AS load_rows FROM dpport.cargo_work_load GROUP BY 1 ORDER BY 1;
