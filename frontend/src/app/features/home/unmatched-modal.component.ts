import { Component, OnInit, computed, inject, output, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { DragDropModule } from '@angular/cdk/drag-drop';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzCheckboxModule } from 'ng-zorro-antd/checkbox';
import { NzIconModule } from 'ng-zorro-antd/icon';
import { NzInputModule } from 'ng-zorro-antd/input';
import { NzModalModule } from 'ng-zorro-antd/modal';
import { NzSpinModule } from 'ng-zorro-antd/spin';
import { NzTooltipModule } from 'ng-zorro-antd/tooltip';
import { NzMessageService } from 'ng-zorro-antd/message';
import { apiErrorMessage } from '../../core/api/api-error';
import { AuthService } from '../../core/auth/auth.service';
import { DICTS } from '../../core/auth/roles';
import { UnmatchedApiService, UnmatchedGroup } from './unmatched-api.service';
import { VagonTrailModalComponent } from './vagon-trail-modal.component';

/**
 * Перемещаемая модалка «Без атрибуции» (карточка «Информация»): гружёные вагоны
 * снимка, не сматченные со справочником marka, группами по комбинации ключа
 * (ОКПО + станция отправления + группа груза) с разворотом до вагона и причиной
 * промаха. «Назначить» (senior/admin — граница словарей) открывает форму:
 * поля атрибуции с подсказкой по тому же ОКПО; галка «в справочник» закрывает
 * комбинацию навсегда, без неё правка живёт до конца рейса вагонов.
 */
@Component({
  selector: 'app-unmatched-modal',
  imports: [
    FormsModule, DragDropModule, NzButtonModule, NzCheckboxModule, NzIconModule,
    NzInputModule, NzModalModule, NzSpinModule, NzTooltipModule, VagonTrailModalComponent,
  ],
  template: `
    <nz-modal [nzVisible]="true" [nzTitle]="ttl" [nzFooter]="null" nzWidth="1080px"
              [nzMask]="false" (nzOnCancel)="closed.emit()">
      <ng-template #ttl>
        <div class="ttl" cdkDrag cdkDragRootElement=".ant-modal-content" cdkDragHandle>
          Вагоны без атрибуции
          @if (total()) { <span class="sub">· {{ total() }} ваг. / {{ groups().length }} комбинаций</span> }
        </div>
      </ng-template>

      <ng-container *nzModalContent>
        <div class="bar">
          <span class="spacer"></span>
          <button nz-button nzType="text" nzSize="small" (click)="load()" [nzLoading]="loading()"
                  nz-tooltip nzTooltipTitle="Обновить">
            <span nz-icon nzType="reload"></span>
          </button>
        </div>

        <nz-spin [nzSpinning]="loading()">
          <div class="dp-tbl-wrap">
            <table class="dp-tbl">
              <thead><tr>
                <th class="c-okpo">ОКПО</th><th>Станция погрузки</th><th class="c-grp">Группа груза</th>
                <th class="c-n">Вагонов</th><th class="c-rsn">Причина</th>
                @if (canAssign) { <th class="c-act"></th> }
              </tr></thead>
              <tbody>
                @for (g of groups(); track g.key) {
                  <tr class="grp" (click)="toggle(g.key)">
                    <td class="num">
                      <span nz-icon [nzType]="isOpen(g.key) ? 'down' : 'right'" class="tw"></span>
                      {{ g.okpo || '—' }}
                    </td>
                    <td class="ell" [title]="g.station_nach || g.station_kod">
                      {{ g.station_nach || (g.station_kod ? 'код ' + g.station_kod : '—') }}
                    </td>
                    <td class="c">{{ g.cargo_group || (g.cargo_s || '—') }}</td>
                    <td class="c num">{{ g.vagon_count }}</td>
                    <td><span class="rsn" nz-tooltip [nzTooltipTitle]="reasonHint(g)">{{ reasonLabel(g) }}</span></td>
                    @if (canAssign) {
                      <td class="c">
                        <button nz-button nzSize="small" (click)="openAssign(g); $event.stopPropagation()"
                                nz-tooltip nzTooltipTitle="Заполнить атрибуцию вагонов комбинации">
                          Назначить
                        </button>
                      </td>
                    }
                  </tr>
                  @if (isOpen(g.key)) {
                    <tr class="vag vag-head"><td></td><td colspan="5">
                      <table class="inner">
                        <thead><tr>
                          <th class="i-vag">Вагон</th><th class="i-idx">Индекс</th><th>Станция операции</th>
                          <th>Операция</th><th class="i-dt">Время</th><th>Груз</th>
                          <th class="i-ves">Вес</th><th class="i-nzn">Назнач.</th><th class="i-dt">Погружен</th>
                        </tr></thead>
                        <tbody>
                          @for (v of g.vagons; track v.id) {
                            <tr>
                              <td class="num">
                                <a class="lnk" (click)="trailFor.set({ id: v.id, vagon: v.vagon })"
                                   nz-tooltip nzTooltipTitle="История движения вагона">{{ v.vagon }}</a>
                              </td>
                              <td class="num">{{ v.index || '—' }}</td>
                              <td class="ell" [title]="v.station_oper">{{ v.station_oper || '—' }}</td>
                              <td class="ell" [title]="v.oper_s">{{ v.oper_s || '—' }}</td>
                              <td class="num">{{ fmtDT(v.time_op) }}</td>
                              <td class="ell" [title]="v.cargo_s || v.code_cargo">
                                {{ v.cargo_s || (v.code_cargo ? 'код ' + v.code_cargo : '—') }}
                              </td>
                              <td class="num">{{ v.ves ?? '—' }}</td>
                              <td class="c">{{ v.naznach || v.gruzpol_s || '—' }}</td>
                              <td class="num">{{ fmtDT(v.date_nach) }}</td>
                            </tr>
                          }
                        </tbody>
                      </table>
                    </td></tr>
                  }
                } @empty {
                  <tr><td [attr.colspan]="canAssign ? 6 : 5" class="empty">
                    @if (loading()) { Загрузка… } @else { Все гружёные вагоны сматчены со справочником }
                  </td></tr>
                }
              </tbody>
            </table>
          </div>
        </nz-spin>
        <p class="hint">
          Строка = комбинация «ОКПО + станция погрузки + группа груза», по которой вагоны не нашлись
          в справочнике Marka; клик — вагоны. Порожние под погрузку не показываются.
          @if (!canAssign) { Назначение атрибуции — старший оператор или администратор. }
        </p>
      </ng-container>
    </nz-modal>

    <!-- Форма назначения атрибуции выбранной комбинации. -->
    @if (assignFor(); as g) {
      <nz-modal [nzVisible]="true" [nzTitle]="attl" nzWidth="460px" [nzMask]="false"
                [nzOkText]="'Назначить'" [nzOkLoading]="saving()" [nzOkDisabled]="!fGruzotpr.trim()"
                (nzOnOk)="submit()" (nzOnCancel)="assignFor.set(null)">
        <ng-template #attl>
          <div class="ttl" cdkDrag cdkDragRootElement=".ant-modal-content" cdkDragHandle>Назначение атрибуции</div>
        </ng-template>
        <ng-container *nzModalContent>
          <div class="keyline">
            ОКПО <b>{{ g.okpo || '—' }}</b> · {{ g.station_nach || ('код ' + (g.station_kod || '—')) }}
            · {{ g.cargo_group || '—' }} · {{ g.vagon_count }} ваг.
          </div>
          <div class="reason">{{ reasonHint(g) }}</div>
          @if (g.suggest; as s) {
            <div class="sugg">
              Подсказка по ОКПО из Marka: <b>{{ s.gruzotpr }}</b>@if (s.client) {, {{ s.client }}}
              <a class="lnk" (click)="applySuggest()">подставить</a>
            </div>
          }
          <label class="fl">Грузоотправитель <span class="req">*</span>
            <input nz-input nzSize="small" [(ngModel)]="fGruzotpr" placeholder="как в Marka (shipper)" />
          </label>
          <label class="fl">Клиент
            <input nz-input nzSize="small" [(ngModel)]="fClient" />
          </label>
          <div class="pair">
            <label class="fl">СМС1
              <input nz-input nzSize="small" [(ngModel)]="fSms1" placeholder="метка отправителя" />
            </label>
            <label class="fl">СМС3
              <input nz-input nzSize="small" [(ngModel)]="fSms3" />
            </label>
          </div>
          <label class="fl">Цвет строки
            <span class="clr">
              <input type="color" [value]="fColor || '#ffffff'" (input)="fColor = $any($event.target).value" />
              <input nz-input nzSize="small" [(ngModel)]="fColor" placeholder="#RRGGBB (пусто — без цвета)" />
              @if (fColor) { <a class="lnk" (click)="fColor = ''">снять</a> }
            </span>
          </label>
          <label nz-checkbox [(ngModel)]="fAddToMarka" [nzDisabled]="!g.can_add_to_marka" class="chk"
                 nz-tooltip [nzTooltipTitle]="g.can_add_to_marka
                   ? 'Комбинация записывается в справочник Marka: следующие загрузки сматчатся сами'
                   : 'В справочник можно добавить только полную комбинацию: числовые ОКПО и код станции, известная группа груза'">
            Добавить комбинацию в справочник Marka
          </label>
          @if (!fAddToMarka) {
            <p class="hint">Без записи в справочник заполнение живёт до конца рейса вагонов:
              новый рейс той же комбинации снова окажется без атрибуции.</p>
          }
        </ng-container>
      </nz-modal>
    }

    @if (trailFor(); as tf) {
      <app-vagon-trail-modal [vagonId]="tf.id" [vagon]="tf.vagon" (closed)="trailFor.set(null)" />
    }
  `,
  styles: [`
    .ttl { cursor: move; user-select: none; }
    .ttl .sub { color: var(--color-text-muted); font-weight: 400; font-size: var(--font-size-sm); }
    .bar { display: flex; align-items: center; margin-bottom: var(--space-xs); }
    .spacer { flex: 1 1 auto; }
    .grp { cursor: pointer; }
    .grp:hover td { background: var(--color-bg-hover); }
    .tw { font-size: 10px; color: var(--color-text-muted); margin-right: 4px; }
    .vag td { background: var(--color-bg-subtle); }
    .inner { width: 100%; border-collapse: collapse; }
    .inner th { text-align: left; font-weight: 500; color: var(--color-text-secondary);
                font-size: var(--font-size-sm); padding: 2px 8px; }
    .inner td { padding: 2px 8px; font-size: var(--font-size-sm); }
    .i-vag { width: 90px; } .i-idx { width: 120px; } .i-dt { width: 96px; }
    .i-ves { width: 60px; } .i-nzn { width: 70px; }
    .c { text-align: center; white-space: nowrap; }
    .num { font-variant-numeric: tabular-nums; white-space: nowrap; }
    .ell { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; max-width: 230px; }
    .c-okpo { width: 120px; } .c-grp { width: 120px; } .c-n { width: 76px; }
    .c-rsn { width: 230px; } .c-act { width: 110px; }
    .rsn { display: inline-block; padding: 0 6px; border-radius: 10px; font-size: 11px;
           background: var(--color-warning-bg); border: 1px solid var(--color-warning); }
    .lnk { color: var(--color-primary-active); text-decoration: underline; cursor: pointer; }
    .empty { text-align: center; color: var(--color-text-secondary); padding: var(--space-md); }
    .hint { margin: var(--space-xs) 0 0; color: var(--color-text-muted); font-size: var(--font-size-sm); }
    /* Форма назначения */
    .keyline { margin-bottom: var(--space-xs); }
    .reason { color: var(--color-text-secondary); font-size: var(--font-size-sm); margin-bottom: var(--space-sm); }
    .sugg { background: var(--color-bg-subtle); border-radius: var(--radius-sm);
            padding: 4px 8px; font-size: var(--font-size-sm); margin-bottom: var(--space-sm); }
    .sugg .lnk { margin-left: 6px; }
    .fl { display: block; margin-bottom: var(--space-sm); font-size: var(--font-size-sm); }
    .fl input[nz-input] { margin-top: 2px; }
    .req { color: var(--color-danger-text); }
    .pair { display: flex; gap: var(--space-sm); }
    .pair .fl { flex: 1; }
    .clr { display: flex; align-items: center; gap: var(--space-sm); margin-top: 2px; }
    .clr input[type=color] { width: 28px; height: 26px; padding: 0; border: 1px solid var(--color-border);
                             border-radius: var(--radius-sm); background: none; cursor: pointer; }
    .chk { margin-top: var(--space-xs); }
  `],
})
export class UnmatchedModalComponent implements OnInit {
  private readonly api = inject(UnmatchedApiService);
  private readonly auth = inject(AuthService);
  private readonly msg = inject(NzMessageService);

  readonly closed = output<void>();
  /** Родитель обновляет счётчик карточки после назначения. */
  readonly reload = output<void>();

  readonly loading = signal(false);
  readonly saving = signal(false);
  readonly groups = signal<UnmatchedGroup[]>([]);
  readonly open = signal<Set<string>>(new Set());
  readonly trailFor = signal<{ id: string; vagon: string } | null>(null);
  readonly assignFor = signal<UnmatchedGroup | null>(null);

  readonly total = computed(() => this.groups().reduce((s, g) => s + g.vagon_count, 0));

  /** Назначение — граница словарей (senior-operator/admin), как правка Marka в Админе. */
  readonly canAssign = this.auth.allows(DICTS);

  // Поля формы назначения (обычные поля: форма живёт в одной вложенной модалке).
  fGruzotpr = '';
  fClient = '';
  fSms1 = '';
  fSms3 = '';
  fColor = '';
  fAddToMarka = false;

  ngOnInit(): void {
    void this.load();
  }

  async load(): Promise<void> {
    this.loading.set(true);
    try {
      this.groups.set(await this.api.getGroups());
    } catch (err) {
      this.msg.error(apiErrorMessage(err));
    } finally {
      this.loading.set(false);
    }
  }

  toggle(key: string): void {
    const next = new Set(this.open());
    next.has(key) ? next.delete(key) : next.add(key);
    this.open.set(next);
  }

  isOpen(key: string): boolean {
    return this.open().has(key);
  }

  reasonLabel(g: UnmatchedGroup): string {
    switch (g.reason) {
      case 'bad_okpo': return 'нет ОКПО в данных';
      case 'bad_station': return 'нет кода станции';
      case 'no_cargo_group': return 'груз вне словаря';
      default: return 'нет комбинации в Marka';
    }
  }

  reasonHint(g: UnmatchedGroup): string {
    switch (g.reason) {
      case 'bad_okpo':
        return 'В данных РЖД нет (или нечисловое) ОКПО грузоотправителя — матч со справочником невозможен, атрибуция только вручную.';
      case 'bad_station':
        return 'В данных РЖД нет (или нечисловой) код станции отправления — матч со справочником невозможен, атрибуция только вручную.';
      case 'no_cargo_group':
        return 'Код груза не найден в словаре «Cargo» — группа груза не определена. Добавьте код в словарь (Админ) и нажмите «Обновить справочники», либо заполните вручную.';
      default: {
        const part = (ok: boolean, name: string) => `${name}: ${ok ? 'в словаре есть' : 'в словаре нет'}`;
        return `Комбинации нет в справочнике Marka (${part(g.okpo_in_marka, 'ОКПО')}; ` +
          `${part(g.station_in_marka, 'станция')}; ${part(g.group_in_marka, 'группа')}). ` +
          'Назначьте атрибуцию с записью в справочник — комбинация закроется навсегда.';
      }
    }
  }

  openAssign(g: UnmatchedGroup): void {
    this.fGruzotpr = g.suggest?.gruzotpr ?? '';
    this.fClient = g.suggest?.client ?? '';
    this.fSms1 = g.suggest?.sms_1 ?? '';
    this.fSms3 = g.suggest?.sms_3 ?? '';
    this.fColor = g.suggest?.color ?? '';
    this.fAddToMarka = g.can_add_to_marka;
    this.assignFor.set(g);
  }

  applySuggest(): void {
    const s = this.assignFor()?.suggest;
    if (!s) return;
    this.fGruzotpr = s.gruzotpr;
    this.fClient = s.client;
    this.fSms1 = s.sms_1;
    this.fSms3 = s.sms_3;
    this.fColor = s.color;
  }

  async submit(): Promise<void> {
    const g = this.assignFor();
    if (!g || !this.fGruzotpr.trim()) return;
    this.saving.set(true);
    try {
      const res = await this.api.assign({
        okpo: g.okpo, station_kod: g.station_kod, cargo_group: g.cargo_group,
        gruzotpr: this.fGruzotpr.trim(), client: this.fClient.trim(),
        sms_1: this.fSms1.trim(), sms_3: this.fSms3.trim(), color: this.fColor.trim(),
        add_to_marka: this.fAddToMarka,
      });
      this.msg.success(
        `Заполнено вагонов: ${res.updated}, строк истории: ${res.history_filled}` +
        (res.marka_saved ? '; комбинация записана в Marka' : ''));
      this.assignFor.set(null);
      await this.load();
      this.reload.emit();
    } catch (err) {
      this.msg.error(apiErrorMessage(err));
    } finally {
      this.saving.set(false);
    }
  }

  /** «2026-07-24T08:05:00» → «24.07 08:05»; пусто → «—». */
  fmtDT(ts: string | null): string {
    if (!ts || ts.length < 16) return '—';
    return `${ts.slice(8, 10)}.${ts.slice(5, 7)} ${ts.slice(11, 16)}`;
  }
}
