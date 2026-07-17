import { Component, input, model, output, signal } from '@angular/core';
import { NzCheckboxModule } from 'ng-zorro-antd/checkbox';
import { NzIconModule } from 'ng-zorro-antd/icon';
import { NzTagModule } from 'ng-zorro-antd/tag';
import { RearrGroup, RearrSubGroup, RearrVagon } from './rearrange-api.service';

/** Событие ПКМ по элементу дерева: браузерное событие + вагоны элемента. */
export interface TreeContextEvent {
  event: MouseEvent;
  group: RearrGroup;
  sub?: RearrSubGroup;
  vagon?: RearrVagon;
  ids: string[];
}

/**
 * Дерево «группа → подгруппа → вагоны» в стиле gtport: чекбоксы на всех
 * уровнях, метки-«булавки» станций окрашены статусом (5 — красный, 10 —
 * зелёный, прочие — синий), подписи «gruzpol_s • naznach». ПКМ отдаётся
 * родителю (меню зависит от вкладки), при этом элемент автовыделяется.
 */
@Component({
  selector: 'app-group-tree',
  imports: [NzCheckboxModule, NzIconModule, NzTagModule],
  template: `
    @for (g of groups(); track g.key) {
      <div class="group" [class.blocked]="disableUnavailable() && !g.available">
        <div class="row g-row" (contextmenu)="onCtx($event, g)">
          <label nz-checkbox
                 [nzChecked]="allChecked(groupIds(g))"
                 [nzIndeterminate]="someChecked(groupIds(g))"
                 [nzDisabled]="disableUnavailable() && !g.available"
                 (nzCheckedChange)="toggleIds(groupIds(g), $event)"></label>
          <span class="toggle" (click)="flip(g.key)">
            <span nz-icon [nzType]="open().has(g.key) ? 'down' : 'right'"></span>
            <span nz-icon nzType="train" nzTheme="fill" class="train"
                  [style.color]="disableUnavailable() ? (g.available ? '#1890ff' : '#ff4d4f') : '#1890ff'"></span>
            <b>{{ g.index_main || g.index || '—' }}</b>
            @if (g.station_nach || g.station_oper) {
              <span class="mut">{{ g.station_nach || g.station_oper }}</span>
            }
            @if (g.pereadr_port) { <nz-tag nzColor="orange">ВП: {{ g.pereadr_port }}</nz-tag> }
            @if (g.naznach) { <nz-tag class="term">{{ g.naznach }}</nz-tag> }
            @if (g.status != null && g.index) { <span class="mut">статус {{ g.status }}</span> }
          </span>
          <span class="spacer"></span>
          @if (disableUnavailable()) {
            <nz-tag [nzColor]="g.available ? 'green' : 'default'">{{ g.available ? 'доступен' : 'недоступен' }}</nz-tag>
          }
          <span class="mut">({{ g.vagon_count }})</span>
        </div>

        @if (open().has(g.key)) {
          @for (sg of g.sub_groups; track sg.key) {
            <div class="sub">
              <div class="row" (contextmenu)="onCtx($event, g, sg)">
                <label nz-checkbox
                       [nzChecked]="allChecked(subIds(sg))"
                       [nzIndeterminate]="someChecked(subIds(sg))"
                       [nzDisabled]="disableUnavailable() && !g.available"
                       (nzCheckedChange)="toggleIds(subIds(sg), $event)"></label>
                <span class="toggle" (click)="flip(g.key + '::' + sg.key)">
                  <span nz-icon [nzType]="open().has(g.key + '::' + sg.key) ? 'down' : 'right'"></span>
                  <span nz-icon nzType="environment" nzTheme="fill" class="pin"
                        [style.color]="statusColor(sg.status)"></span>
                  {{ sg.station_oper || sg.station_nach || '—' }}
                  @if (sg.index || sg.index_main) { <span class="mut">{{ sg.index || sg.index_main }}</span> }
                  <span class="mut">{{ sg.gruzpol_s || '—' }} • {{ sg.naznach || '—' }}</span>
                  @if (sg.rasst_stan_nazn) { <span class="mut">{{ sg.rasst_stan_nazn }} км</span> }
                </span>
                <span class="spacer"></span>
                <span class="mut">({{ sg.vagon_count }})</span>
              </div>
              @if (open().has(g.key + '::' + sg.key)) {
                <div class="vagons">
                  @for (v of sg.vagons; track v.id) {
                    <span class="chip" [class.sel]="selected().has(v.id)"
                          (click)="disableUnavailable() && !g.available ? null : toggleIds([v.id], !selected().has(v.id))"
                          (contextmenu)="onCtx($event, g, sg, v)"
                          [title]="'накладная ' + (v.invoice || '—') + ' · ' + v.gruzpol_s + ' • ' + v.naznach">
                      {{ v.npp_vag ?? '·' }} | {{ v.vagon }}
                    </span>
                  }
                </div>
              }
            </div>
          }
        }
      </div>
    }
  `,
  styles: [`
    .group { border-bottom: 1px solid var(--color-border, #f0f0f0); }
    .group.blocked { opacity: .6; }
    .row { display: flex; align-items: center; gap: 8px; padding: 5px 8px; }
    .g-row { background: var(--color-bg-container-secondary, #fafafa); }
    .toggle { cursor: pointer; display: inline-flex; align-items: center; gap: 6px; min-width: 0; flex-wrap: wrap; }
    .spacer { flex: 1 1 auto; }
    .mut { color: var(--color-text-secondary); font-size: var(--font-size-sm); }
    .term { margin: 0; }
    .train { font-size: 14px; }
    .pin { font-size: 12px; }
    .sub { margin-left: 26px; border-top: 1px dashed var(--color-border, #f0f0f0); }
    .vagons { display: flex; flex-wrap: wrap; gap: 4px; padding: 4px 8px 8px 34px; }
    .chip { font-size: var(--font-size-sm); font-variant-numeric: tabular-nums; padding: 1px 6px;
            border: 1px solid var(--color-border, #d9d9d9); border-radius: 4px; cursor: pointer; }
    .chip.sel { background: var(--color-primary, #1677ff); color: #fff; border-color: var(--color-primary, #1677ff); }
  `],
})
export class GroupTreeComponent {
  readonly groups = input<RearrGroup[]>([]);
  /** true — недоступные (available=false) группы блокируются (переадресация). */
  readonly disableUnavailable = input(false);
  /** Выбранные id вагонов — двусторонняя модель (родитель применяет операции). */
  readonly selected = model<Set<string>>(new Set());
  /** ПКМ по элементу: родитель показывает своё меню; элемент уже автовыделен. */
  readonly ctx = output<TreeContextEvent>();

  readonly open = signal<Set<string>>(new Set());

  groupIds(g: RearrGroup): string[] {
    return g.sub_groups.flatMap((sg) => sg.vagons.map((v) => v.id));
  }
  subIds(sg: RearrSubGroup): string[] {
    return sg.vagons.map((v) => v.id);
  }
  allChecked(ids: string[]): boolean {
    return ids.length > 0 && ids.every((id) => this.selected().has(id));
  }
  someChecked(ids: string[]): boolean {
    const n = ids.filter((id) => this.selected().has(id)).length;
    return n > 0 && n < ids.length;
  }
  toggleIds(ids: string[], checked: boolean): void {
    const next = new Set(this.selected());
    for (const id of ids) checked ? next.add(id) : next.delete(id);
    this.selected.set(next);
  }
  flip(key: string): void {
    const next = new Set(this.open());
    next.has(key) ? next.delete(key) : next.add(key);
    this.open.set(next);
  }

  statusColor(status: number | null | undefined): string {
    switch (status) {
      case 5: return '#ff4d4f';
      case 10: return '#52c41a';
      default: return '#1890ff';
    }
  }

  /** ПКМ: автовыделение элемента под курсором (как в gtport) + событие родителю. */
  onCtx(event: MouseEvent, group: RearrGroup, sub?: RearrSubGroup, vagon?: RearrVagon): void {
    event.preventDefault();
    if (this.disableUnavailable() && !group.available) return;
    const ids = vagon ? [vagon.id] : sub ? this.subIds(sub) : this.groupIds(group);
    this.toggleIds(ids, true);
    this.ctx.emit({ event, group, sub, vagon, ids });
  }
}
