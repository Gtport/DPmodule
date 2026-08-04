// Генератор golden-фикстур для internal/service/unloadsim.
//
// Исполняет ЭТАЛОННЫЙ код симуляции выгрузки из старого GTport
// (~/projects/DP/gtlogic/client/src/components/gt — только чтение) на
// синтетических сценариях и пишет пары вход/выход в
// internal/service/unloadsim/testdata/*.json.
//
// Запуск (из корня DPmodule):
//   npx tsx scripts/golden/gen_gtsim_fixtures.mts
//
// Фикстуры коммитятся в git; скрипт нужен только для регенерации при
// изменении набора сценариев. Цикл по дням здесь — точная копия
// client/src/components/gt/hooks/useUnloading.ts (без React).

import * as fs from 'node:fs';
import * as path from 'node:path';
import { fileURLToPath } from 'node:url';

const GT = path.join(
  path.dirname(fileURLToPath(import.meta.url)),
  '../../../gtlogic/client/src/components/gt'
);

const { prepareDayData, simulateDayUnloading, simulateNormUnloading } =
  await import(path.join(GT, 'simulation.ts'));
const { railwayToCalcTime } = await import(path.join(GT, 'utils.ts'));
const { MAX_TRAIN_WAGONS } = await import(path.join(GT, 'constants.ts'));

// ─── Вход сценария ───────────────────────────────────────────────────────────

interface FixtureSubGroup {
  key: string;
  naznach: string;
  cargo_group: string;
  vagon_count: number;
  color: string;
  station_nach: string;
  index_main: string;
  gruzpol_s: string;
}

interface FixtureTrain {
  index: string;
  status: string;
  prog_jd: string; // ЖД-время naive "YYYY-MM-DDTHH:MM:SS"
  sub_groups: FixtureSubGroup[];
}

interface Scenario {
  name: string;
  comment: string;
  port: 'АЭ' | 'УТ-1' | 'ГУТ-2';
  startDate: string; // YYYY-MM-DD
  days: number;
  speeds: Record<string, { default: number; userDefined?: Record<string, number> }>;
  norms: Record<string, number>;
  initialRemainders: Record<string, number>; // `${port}_${cargo}` → вагонов
  trains: FixtureTrain[];
}

// ─── Прогон эталона: копия цикла useUnloading.ts ────────────────────────────

function runScenario(sc: Scenario) {
  // Поезда эталона: calcTime из prog_jd, sub_groups как есть
  const trains = sc.trains.map((t) => ({
    ...t,
    vagon_count: t.sub_groups.reduce((s, sg) => s + sg.vagon_count, 0),
    calcTime: railwayToCalcTime(t.prog_jd),
    originalProgJd: t.prog_jd,
  }));

  // Группировка по `${port}_${cargoType}` — копия useUnloading.ts:35-52
  const groupedTrains: Record<string, any[]> = {};
  trains.forEach((train: any) => {
    train.sub_groups.forEach((subGroup: any) => {
      if (subGroup.naznach !== sc.port) return;
      let cargoType = 'ОБЩИЙ';
      if (sc.port === 'ГУТ-2') {
        if (['УГОЛЬ', 'МЕТАЛЛ', 'ЧУГУН'].includes(subGroup.cargo_group)) {
          cargoType = subGroup.cargo_group;
        } else return;
      }
      const key = `${sc.port}_${cargoType}`;
      (groupedTrains[key] ||= []).push({ ...train, sub_groups: [subGroup] });
    });
  });

  const [year, month, day] = sc.startDate.split('-').map(Number);
  const startDateTime = new Date(Date.UTC(year, month - 1, day, 0, 0, 0, 0));

  // DEFAULT_NORM эталона захардкожен под боевые ключи; в фикстурах нормы
  // задаются сценарием, поэтому подменяем normSpeed после prepareDayData.
  const applyNorm = (pd: any, key: string) => {
    const norm = sc.norms[key];
    if (norm !== undefined) {
      pd.normSpeed = norm;
      pd.normSpeedPerHour = norm / 24;
    }
  };

  const days: any[] = [];
  let prevSingle: any | undefined;
  const prevByCargo: Record<string, any> = {};

  for (let dayIdx = 0; dayIdx < sc.days; dayIdx++) {
    const cargoTypes = sc.port === 'ГУТ-2' ? ['УГОЛЬ', 'МЕТАЛЛ', 'ЧУГУН'] : ['ОБЩИЙ'];
    for (const cargoType of cargoTypes) {
      const prev = sc.port === 'ГУТ-2' ? prevByCargo[cargoType] : prevSingle;
      const preparedDays = prepareDayData(dayIdx, startDateTime, sc.port, groupedTrains, sc.speeds, prev);
      const preparedDay = preparedDays.find((d: any) => d.cargoType === cargoType);
      if (!preparedDay) continue;
      const key = `${sc.port}_${cargoType}`;
      applyNorm(preparedDay, key);

      if (dayIdx === 0 && sc.initialRemainders[key] !== undefined) {
        preparedDay.incomingRemainder = sc.initialRemainders[key];
      }

      const sim = simulateDayUnloading(preparedDay);
      if (sc.port === 'ГУТ-2') prevByCargo[cargoType] = sim;
      else prevSingle = sim;

      const usefulFormation = simulateNormUnloading(preparedDay);
      const clamp = (t: any) => Math.min(t.sub_groups[0].vagon_count, MAX_TRAIN_WAGONS);
      const incomingTotal = preparedDay.incomingRemainder +
        preparedDay.incomingTrains.reduce((s: number, t: any) => s + clamp(t), 0);
      const arrival = preparedDay.arrivingTrains.reduce((s: number, t: any) => s + clamp(t), 0);
      const remaining = sim.remainingRemainder +
        sim.remainingTrains.reduce((s: number, t: any) => s + clamp(t), 0);

      days.push({
        date: preparedDay.date,
        cargo_type: cargoType,
        speed: preparedDay.planSpeed,
        norm_speed: preparedDay.normSpeed,
        incoming_total: incomingTotal,
        arrival,
        total_formation: incomingTotal + arrival,
        useful_formation: usefulFormation,
        unloaded: sim.unloadedToday,
        remaining,
        total_wait_min: round6(sim.totalWaitTime),
        remainder_out: sim.remainingRemainder,
        remainder_out_index: sim.remainingRemainderTrainIndex || '',
        carried_over: sim.remainingTrains.map((t: any) => ({
          index: t.index,
          wagons: t.sub_groups[0].vagon_count,
          subgroup_key: t.sub_groups[0].key,
        })),
        operations: sim.operations.map(normalizeOp),
      });
    }
  }
  return days;
}

function normalizeOp(op: any) {
  return {
    train_index: op.trainIndex,
    train_name: op.trainName,
    station_nach: op.stationNach || '',
    index_main: op.indexMain || '',
    start_calc: isoNaive(op.startTime),
    end_calc: isoNaive(op.endTime),
    start_jd: isoNaive(op.startRailwayTime),
    end_jd: isoNaive(op.endRailwayTime),
    wagons: op.wagons,
    total_wagons: op.totalWagons ?? 0,
    color: op.color,
    is_remainder: !!op.isRemainder,
    is_carried_over: !!op.isCarriedOver,
    is_partial: !!op.isPartialRemainder,
    wait_min: round6(op.waitTime ?? 0),
    original_arrival_jd: op.originalArrivalTime ? isoNaive(op.originalArrivalTime) : '',
  };
}

// ISO UTC → naive "YYYY-MM-DDTHH:MM:SS.mmm" (в DPmodule время без TZ)
function isoNaive(iso: string): string {
  const d = new Date(iso.includes('Z') ? iso : iso + 'Z');
  return d.toISOString().replace('Z', '');
}

function round6(x: number): number {
  return Math.round(x * 1e6) / 1e6;
}

// ─── Сценарии ────────────────────────────────────────────────────────────────
// Времена prog_jd — ЖД-шкала (сдвиг −18ч/+6ч даёт расчётную шкалу).
// ЖД 01:30 → расчёт 07:30 того же дня; ЖД 19:00 → расчёт 01:00 того же дня.

const sg = (o: Partial<FixtureSubGroup>): FixtureSubGroup => ({
  key: 'sg-' + Math.abs(hash(JSON.stringify(o))).toString(36),
  naznach: 'АЭ', cargo_group: 'УГОЛЬ', vagon_count: 0, color: '#0070C0',
  station_nach: 'ЕРУНАКОВО', index_main: '8650-123-9840', gruzpol_s: 'АЭ', ...o,
});
function hash(s: string): number {
  let h = 0;
  for (let i = 0; i < s.length; i++) h = (h * 31 + s.charCodeAt(i)) | 0;
  return h;
}

const scenarios: Scenario[] = [
  {
    name: 'ae_basic',
    comment: 'АЭ 3 дня: входящий остаток выгружается целиком; простой >15 мин и ≤15 мин; финальный простой; пустые сутки',
    port: 'АЭ', startDate: '2026-08-04', days: 3,
    speeds: { 'АЭ_ОБЩИЙ': { default: 130 } },
    norms: { 'АЭ_ОБЩИЙ': 144 },
    initialRemainders: { 'АЭ_ОБЩИЙ': 40 },
    trains: [
      // расчёт 07:30 (остаток кончится в 07:23:04.615 → ожидание 6.9 мин, БЕЗ операции простоя)
      { index: '8650-101-9840', status: '2', prog_jd: '2026-08-04T01:30:00', sub_groups: [sg({ key: 'a1', vagon_count: 45, color: '#7030A0' })] },
      // расчёт 16:30 (выгрузка первого кончится в 15:48:27.692 → простой 41.5 мин)
      { index: '8650-102-9840', status: '2', prog_jd: '2026-08-04T10:30:00', sub_groups: [sg({ key: 'a2', vagon_count: 30, color: '#00B0F0' })] },
    ],
  },
  {
    name: 'ae_remainder_split',
    comment: 'АЭ 3 дня: остаток 200 не успевает за сутки — делится на части, поезда дня уходят целиком на завтра',
    port: 'АЭ', startDate: '2026-08-04', days: 3,
    speeds: { 'АЭ_ОБЩИЙ': { default: 130 } },
    norms: { 'АЭ_ОБЩИЙ': 144 },
    initialRemainders: { 'АЭ_ОБЩИЙ': 200 },
    trains: [
      { index: '8650-111-9840', status: '2', prog_jd: '2026-08-04T02:00:00', sub_groups: [sg({ key: 'b1', vagon_count: 55 })] },
      { index: '8650-112-9840', status: '5', prog_jd: '2026-08-04T12:00:00', sub_groups: [sg({ key: 'b2', vagon_count: 48, color: '#4CAF50' })] },
    ],
  },
  {
    name: 'ae_partial_train_clamp',
    comment: 'АЭ 3 дня: поезд 71 ваг → отцепка до 63; поздний поезд выгружается частично и переносится; следующий не начинает выгрузку',
    port: 'АЭ', startDate: '2026-08-04', days: 3,
    speeds: { 'АЭ_ОБЩИЙ': { default: 130 } },
    norms: { 'АЭ_ОБЩИЙ': 144 },
    initialRemainders: {},
    trains: [
      { index: '8650-121-9840', status: '2', prog_jd: '2026-08-04T00:10:00', sub_groups: [sg({ key: 'c1', vagon_count: 71, color: '#FF5722' })] },
      // расчёт 20:00 — на выгрузку 55 нужно 10.15ч, до конца суток ~4ч → частично
      { index: '8650-122-9840', status: '2', prog_jd: '2026-08-04T14:00:00', sub_groups: [sg({ key: 'c2', vagon_count: 55, color: '#7030A0' })] },
      // расчёт 23:00 — не успеет начать (очередь занята) → целиком на завтра
      { index: '8650-123-9840', status: '2', prog_jd: '2026-08-04T17:00:00', sub_groups: [sg({ key: 'c3', vagon_count: 40 })] },
    ],
  },
  {
    name: 'ae_multi_subgroup',
    comment: 'АЭ 2 дня: сборный поезд — две подгруппы одного потока → две записи с одним index; дедупликация untouched по ключу подгруппы',
    port: 'АЭ', startDate: '2026-08-04', days: 2,
    speeds: { 'АЭ_ОБЩИЙ': { default: 130 } },
    norms: { 'АЭ_ОБЩИЙ': 144 },
    initialRemainders: {},
    trains: [
      {
        index: '8650-131-9840', status: '2', prog_jd: '2026-08-04T13:00:00',
        sub_groups: [
          sg({ key: 'd1', vagon_count: 35, color: '#7030A0' }),
          sg({ key: 'd2', vagon_count: 28, color: '#00B0F0' }),
        ],
      },
      { index: '8650-132-9840', status: '2', prog_jd: '2026-08-04T16:30:00', sub_groups: [sg({ key: 'd3', vagon_count: 50 })] },
    ],
  },
  {
    name: 'gut_three_flows',
    comment: 'ГУТ-2 4 дня: три независимых потока УГОЛЬ/МЕТАЛЛ/ЧУГУН; сборный поезд уголь+металл; остатки по потокам; чугун медленный (30/сут)',
    port: 'ГУТ-2', startDate: '2026-08-04', days: 4,
    speeds: {
      'ГУТ-2_УГОЛЬ': { default: 120 },
      'ГУТ-2_МЕТАЛЛ': { default: 100 },
      'ГУТ-2_ЧУГУН': { default: 30 },
    },
    norms: { 'ГУТ-2_УГОЛЬ': 168, 'ГУТ-2_МЕТАЛЛ': 84, 'ГУТ-2_ЧУГУН': 30 },
    initialRemainders: { 'ГУТ-2_УГОЛЬ': 25, 'ГУТ-2_ЧУГУН': 44 },
    trains: [
      {
        index: '8650-201-9840', status: '2', prog_jd: '2026-08-04T03:00:00',
        sub_groups: [
          sg({ key: 'e1', naznach: 'ГУТ-2', cargo_group: 'УГОЛЬ', vagon_count: 40, gruzpol_s: 'ГУТ-2', color: '#00B0F0' }),
          sg({ key: 'e2', naznach: 'ГУТ-2', cargo_group: 'МЕТАЛЛ', vagon_count: 22, gruzpol_s: 'ГУТ-2', color: '#FF5722' }),
        ],
      },
      { index: '8650-202-9840', status: '2', prog_jd: '2026-08-04T20:30:00', sub_groups: [sg({ key: 'e3', naznach: 'ГУТ-2', cargo_group: 'МЕТАЛЛ', vagon_count: 57, gruzpol_s: 'ГУТ-2', color: '#FF5722' })] },
      { index: '8650-203-9840', status: '2', prog_jd: '2026-08-05T06:00:00', sub_groups: [sg({ key: 'e4', naznach: 'ГУТ-2', cargo_group: 'ЧУГУН', vagon_count: 36, gruzpol_s: 'ГУТ-2', color: '#9C27B0' })] },
      { index: '8650-204-9840', status: '2', prog_jd: '2026-08-05T11:00:00', sub_groups: [sg({ key: 'e5', naznach: 'ГУТ-2', cargo_group: 'УГОЛЬ', vagon_count: 63, gruzpol_s: 'ГУТ-2', color: '#0070C0' })] },
      // подгруппа чужого порта — должна отфильтроваться
      { index: '8650-205-9840', status: '2', prog_jd: '2026-08-05T12:00:00', sub_groups: [sg({ key: 'e6', naznach: 'АЭ', vagon_count: 50 })] },
    ],
  },
  {
    name: 'ut1_chain_speed_override',
    comment: 'УТ-1 5 дней: цепочка переносов; пользовательская скорость на второй день (userDefined); высокая норма 432',
    port: 'УТ-1', startDate: '2026-08-04', days: 5,
    speeds: { 'УТ-1_ОБЩИЙ': { default: 250, userDefined: { '2026-08-05': 60 } } },
    norms: { 'УТ-1_ОБЩИЙ': 432 },
    initialRemainders: { 'УТ-1_ОБЩИЙ': 110 },
    trains: [
      { index: '9840-301-8650', status: '2', prog_jd: '2026-08-04T01:41:00', sub_groups: [sg({ key: 'f1', naznach: 'УТ-1', vagon_count: 63, gruzpol_s: 'УТ-1', color: '#7030A0' })] },
      { index: '9840-302-8650', status: '2', prog_jd: '2026-08-04T08:43:00', sub_groups: [sg({ key: 'f2', naznach: 'УТ-1', vagon_count: 58, gruzpol_s: 'УТ-1', color: '#00B0F0' })] },
      { index: '9840-303-8650', status: '2', prog_jd: '2026-08-05T05:38:00', sub_groups: [sg({ key: 'f3', naznach: 'УТ-1', vagon_count: 66, gruzpol_s: 'УТ-1', color: '#4CAF50' })] },
      { index: '9840-304-8650', status: '2', prog_jd: '2026-08-05T21:00:00', sub_groups: [sg({ key: 'f4', naznach: 'УТ-1', vagon_count: 61, gruzpol_s: 'УТ-1', color: '#FF5722' })] },
      { index: '9840-305-8650', status: '2', prog_jd: '2026-08-06T15:53:00', sub_groups: [sg({ key: 'f5', naznach: 'УТ-1', vagon_count: 55, gruzpol_s: 'УТ-1', color: '#9C27B0' })] },
    ],
  },
  {
    name: 'norm_remainder_overflow',
    comment: 'Полезное образование: остаток не успевает даже по норме — ранний выход simulateNormUnloading',
    port: 'АЭ', startDate: '2026-08-04', days: 2,
    speeds: { 'АЭ_ОБЩИЙ': { default: 50 } },
    norms: { 'АЭ_ОБЩИЙ': 60 },
    initialRemainders: { 'АЭ_ОБЩИЙ': 90 },
    trains: [
      { index: '8650-141-9840', status: '2', prog_jd: '2026-08-04T05:00:00', sub_groups: [sg({ key: 'g1', vagon_count: 30 })] },
    ],
  },
];

// ─── Запись ─────────────────────────────────────────────────────────────────

const outDir = path.join(
  path.dirname(fileURLToPath(import.meta.url)),
  '../../internal/service/unloadsim/testdata'
);
fs.mkdirSync(outDir, { recursive: true });

for (const sc of scenarios) {
  const output = runScenario(sc);
  const fixture = {
    comment: sc.comment,
    input: {
      port: sc.port,
      start_date: sc.startDate,
      days: sc.days,
      speeds: sc.speeds,
      norms: sc.norms,
      initial_remainders: sc.initialRemainders,
      trains: sc.trains,
    },
    expected_days: output,
  };
  const file = path.join(outDir, `${sc.name}.json`);
  fs.writeFileSync(file, JSON.stringify(fixture, null, 2) + '\n');
  console.log(`✓ ${sc.name}: дней ${output.length}, операций ${output.reduce((s, d) => s + d.operations.length, 0)} → ${file}`);
}
console.log('Готово.');
