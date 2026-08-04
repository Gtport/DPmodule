import { Component, computed, input, output } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { GtDay, GtFlow, GtOperation } from './gt-forecast-api.service';

/**
 * Диаграмма Ганта прогноза выгрузки одного потока (терминал × груз) — перенос
 * gtport GanttChart. Строка = расчётные ЖД-сутки (18:00→18:00), блоки —
 * absolute-позиционирование в процентах суток (как DOM-подход эталона).
 *
 * Колонки чисел: Приб (прибыло за сутки), Полн (полное образование), Полез
 * (полезное образование по норме; красное, если ниже плана), Выгр, Ост
 * (красный при >100), Пр (простой ЧЧ:ММ, красный при >0). «План» —
 * редактируемая скорость суток, правка уходит родителю на пересчёт.
 */

/** Подготовленный блок шкалы. */
interface Block {
  left: number; // %
  width: number; // %
  label: string;
  bg: string;
  border: string;
  textColor: string;
  isWait: boolean;
  title: string;
}

const HOUR_LABELS = ['18', '21', '0', '3', '6', '9', '12', '15'];

@Component({
  selector: 'app-gt-gantt',
  imports: [FormsModule],
  template: `
    <div class="gantt">
      <div class="head" [style.background]="flow().color || '#f5f5f5'">
        <span class="name">{{ flow().terminal }}{{ flow().cargo_key ? ' · ' + flow().cargo_key : '' }}</span>
        <span class="ost">остаток: {{ flow().initial_remainder }}</span>
      </div>
      <table>
        <thead>
          <tr>
            <th class="c-date">Дата</th>
            <th class="c-num" title="Плановая скорость, ваг/сут">План</th>
            <th class="c-scale">
              <div class="scale">
                @for (h of hourLabels; track $index) {
                  <span [style.left.%]="$index * 12.5">{{ h }}</span>
                }
              </div>
            </th>
            <th class="c-num" title="Прибыло за сутки">Приб</th>
            <th class="c-num" title="Полное образование">Полн</th>
            <th class="c-num" title="Полезное образование (по норме)">Полез</th>
            <th class="c-num" title="Выгружено">Выгр</th>
            <th class="c-num" title="Остаток на конец суток">Ост</th>
            <th class="c-num" title="Простой, ЧЧ:ММ">Пр</th>
          </tr>
        </thead>
        <tbody>
          @for (d of days(); track d.day.date) {
            <tr>
              <td class="c-date">{{ d.dateLabel }}</td>
              <td class="c-num">
                <input class="speed" type="number" min="1"
                       [ngModel]="d.day.plan_speed"
                       (ngModelChange)="onSpeed(d.day, $event)" />
              </td>
              <td class="c-scale">
                <div class="lane">
                  @for (b of d.blocks; track $index) {
                    <div class="blk" [class.wait]="b.isWait"
                         [style.left.%]="b.left" [style.width.%]="b.width"
                         [style.background]="b.bg" [style.border-color]="b.border"
                         [style.color]="b.textColor" [title]="b.title">
                      {{ b.label }}
                    </div>
                  }
                </div>
              </td>
              <td class="c-num">{{ d.day.arrival || '' }}</td>
              <td class="c-num b">{{ d.day.total_formation || '' }}</td>
              <td class="c-num" [class.bad]="d.day.useful_formation < d.day.plan_speed">
                {{ d.day.useful_formation || '' }}
              </td>
              <td class="c-num">{{ d.day.unloaded || '' }}</td>
              <td class="c-num" [class.b]="d.day.remaining > 0" [class.bad]="d.day.remaining > 100">
                {{ d.day.remaining || '' }}
              </td>
              <td class="c-num" [class.bad]="d.day.total_wait_min > 0">{{ d.waitLabel }}</td>
            </tr>
          }
        </tbody>
      </table>
    </div>
  `,
  styles: [`
    .gantt { background: var(--color-bg-surface); border-radius: var(--radius-card);
             box-shadow: var(--shadow-card); overflow: hidden; }
    .head { display: flex; align-items: center; gap: var(--space-sm);
            padding: 2px var(--space-sm); font-size: 12px; }
    .head .name { font-weight: 600; }
    .head .ost { margin-left: auto; }
    table { width: 100%; border-collapse: collapse; font-size: 11px; }
    th, td { border: 1px solid var(--color-border, #e5e5e5); padding: 0 2px; text-align: center; }
    th { font-weight: 500; background: var(--color-bg-muted, #fafafa); }
    .c-date { width: 44px; white-space: nowrap; }
    .c-num { width: 40px; }
    .c-num.b, .b { font-weight: 600; }
    .bad { color: #d32f2f; font-weight: 600; }
    .c-scale { min-width: 340px; padding: 0; }
    .scale { position: relative; height: 14px; }
    .scale span { position: absolute; transform: translateX(-50%); font-weight: 400; color: #888; }
    .lane { position: relative; height: 20px; }
    .blk { position: absolute; top: 1px; bottom: 1px; border: 1px solid; border-radius: 2px;
           font-size: 10px; line-height: 16px; overflow: hidden; white-space: nowrap;
           text-overflow: clip; text-align: center; cursor: default; }
    .blk.wait { background: #fff; border-color: #bbb; color: #888; }
    .speed { width: 100%; min-width: 34px; box-sizing: border-box; border: 1px solid transparent;
             text-align: center; font-size: 11px; background: transparent; -moz-appearance: textfield; }
    .speed::-webkit-outer-spin-button, .speed::-webkit-inner-spin-button { -webkit-appearance: none; margin: 0; }
    .speed:hover, .speed:focus { border-color: var(--color-border, #d9d9d9); background: #fff; }
  `],
})
export class GtGanttComponent {
  readonly flow = input.required<GtFlow>();
  /** Правка скорости суток: родитель кладёт её в overrides и пересчитывает. */
  readonly speedChange = output<{ date: string; value: number }>();

  readonly hourLabels = HOUR_LABELS;

  readonly days = computed(() =>
    this.flow().days.map((day) => ({
      day,
      dateLabel: day.date.slice(8, 10) + '.' + day.date.slice(5, 7),
      waitLabel: day.total_wait_min > 0 ? hm(day.total_wait_min) : '',
      blocks: day.operations.map((op) => toBlock(day, op)),
    }))
  );

  onSpeed(day: GtDay, value: number | null): void {
    if (!value || value <= 0 || value === day.plan_speed) return;
    this.speedChange.emit({ date: day.date, value });
  }
}

/** Позиция времени в сутках, % (расчётная шкала — календарные сутки). */
function pct(iso: string, date: string): number {
  const h = +iso.slice(11, 13), m = +iso.slice(14, 16), s = +iso.slice(17, 19);
  let sec = h * 3600 + m * 60 + s;
  if (iso.slice(0, 10) !== date) sec = iso.slice(0, 10) < date ? 0 : 86400;
  return (sec / 86400) * 100;
}

function toBlock(day: GtDay, op: GtOperation): Block {
  const left = pct(op.start_calc, day.date);
  const width = Math.max(pct(op.end_calc, day.date) - left, 0.3);
  if (op.wait_min > 0 && !op.wagons) {
    return {
      left, width, isWait: true, bg: '#fff', border: '#bbb', textColor: '#888',
      label: width > 4 && op.wait_min > 30 ? hm(op.wait_min) : '',
      title: `Простой ${hm(op.wait_min)} (${jd(op.start_jd)} – ${jd(op.end_jd)} ЖД)`,
    };
  }
  const base = op.color || '#4CAF50';
  const label = op.is_remainder
    ? (op.orig_index ? shortIndex(op.orig_index) + ' ост' : 'ост')
    : shortIndex(op.train_index) + (op.is_carried_over ? '*' : '');
  const parts = [
    `${op.train_name}`,
    `Вагонов: ${op.wagons}${op.total_wagons && op.total_wagons !== op.wagons ? ' / ' + op.total_wagons : ''}`,
    op.station_nach ? `ст. ${op.station_nach}` : '',
    op.index_main ? `Р.И.: ${op.index_main}` : '',
    `выгрузка ${jd(op.start_jd)} – ${jd(op.end_jd)} ЖД`,
    op.original_arrival_jd ? `прибытие ${jd(op.original_arrival_jd)} ЖД` : '',
    op.is_partial ? 'выгружен частично, остаток — завтра' : '',
    op.is_carried_over ? 'перенесён с прошлых суток' : '',
  ].filter(Boolean);
  return {
    left, width, isWait: false,
    bg: pastel(base), border: base, textColor: '#222',
    label: width > 3 ? label : '',
    title: parts.join('\n'),
  };
}

/** Короткий индекс поезда: цифры 6–8 либо «с.ф.» (эталон getTrainDisplayIndex). */
function shortIndex(index: string): string {
  if (!index) return '???';
  if (index.includes('с.ф.')) return 'с.ф.';
  if (index.length >= 8) return index.substring(5, 8);
  return index;
}

/** Пастель от цвета клиента: смешивание с белым (эталон toPastel). */
function pastel(hex: string): string {
  if (!/^#[0-9a-fA-F]{6}$/.test(hex)) return '#eee';
  const mix = (i: number) =>
    Math.round(parseInt(hex.slice(i, i + 2), 16) * 0.35 + 255 * 0.65);
  return `rgb(${mix(1)}, ${mix(3)}, ${mix(5)})`;
}

function hm(minutes: number): string {
  const m = Math.round(minutes);
  return `${String(Math.floor(m / 60)).padStart(2, '0')}:${String(m % 60).padStart(2, '0')}`;
}

function jd(iso: string): string {
  return `${iso.slice(8, 10)}.${iso.slice(5, 7)} ${iso.slice(11, 16)}`;
}
