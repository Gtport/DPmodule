import { Component, OnDestroy, OnInit, computed, inject, output, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { DragDropModule } from '@angular/cdk/drag-drop';
import { NzAlertModule } from 'ng-zorro-antd/alert';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzInputModule } from 'ng-zorro-antd/input';
import { NzIconModule } from 'ng-zorro-antd/icon';
import { NzModalModule } from 'ng-zorro-antd/modal';
import { NzTagModule } from 'ng-zorro-antd/tag';
import { NzTooltipModule } from 'ng-zorro-antd/tooltip';
import { NzMessageService } from 'ng-zorro-antd/message';
import { apiErrorMessage } from '../../core/api/api-error';
import {
  DislocationApiService, LKRobotAccount, LKRobotItem, LKRobotState,
} from '../dislocation/dislocation-api.service';
import { LkProcessResultComponent } from './lk-process-result.component';

/**
 * Перемещаемая модалка «АВТО ЛК» — робот сам заходит в кабинет РЖД, забирает
 * дислокацию по каждому потоку и СРАЗУ обновляет дислокацию, если комплект
 * прошёл контроль (решение владельца 01.08.2026: диспетчеру — один клик вместо
 * трёх). Ручная загрузка файлов осталась в соседней модалке «ЛК».
 *
 * Файлов здесь нет вовсе (решение владельца 03.08.2026): робот читает дислокацию
 * прямо из ответа кабинета, xlsx не качает и в приём ничего не кладёт. Поэтому
 * окно показывает срез и число вагонов по каждому кабинету, а не список файлов.
 *
 * Пароли вводятся здесь и никуда не сохраняются — на каждый поток свой аккаунт.
 *
 * ⚠️ Запуск ФОНОВЫЙ: кнопка отправляет запрос, который отвечает сразу, а ход
 * работы окно опрашивает (`robotState`). Поэтому окно можно закрыть и открыть
 * заново — прогресс живёт на сервере, а не в браузере, и ни один прокси перед
 * бэкендом не может оборвать «слишком долгий» запрос.
 */
@Component({
  selector: 'app-lk-robot-modal',
  imports: [
    DragDropModule, FormsModule, NzAlertModule, NzButtonModule, NzIconModule, NzInputModule,
    NzModalModule, NzTagModule, NzTooltipModule, LkProcessResultComponent,
  ],
  template: `
    <nz-modal [nzVisible]="true" [nzTitle]="title" [nzFooter]="null" nzWidth="560px"
              [nzMask]="false" (nzOnCancel)="closed.emit()">
      <ng-template #title>
        <div class="ttl" cdkDrag cdkDragRootElement=".ant-modal-content" cdkDragHandle>
          АВТО ЛК — забор из кабинета РЖД
        </div>
      </ng-template>
      <ng-container *nzModalContent>
        @if (!accounts().length) {
          <p class="muted">Аккаунты ЛК не заведены — автозабор недоступен. Логины заводятся
            в Админе (таблица lk_account), пароль вводит диспетчер при запуске.</p>
        } @else {
          <div class="bar">
            <span class="hint">Пароли не сохраняются — вводятся на один запуск.</span>
            <span class="spacer"></span>
            <button nz-button nzType="text" nzSize="small" nz-tooltip
                    nzTooltipTitle="Обновить состояние" (click)="load()">
              <span nz-icon nzType="reload"></span>
            </button>
          </div>

          <!-- Поток = кабинет одного грузополучателя: слева подпись, справа либо
               поле пароля (пока не бежим), либо состояние этого потока. -->
          @for (a of accounts(); track a.okpo) {
            <div class="rrow">
              <span class="rname" [title]="'логин ' + a.login">{{ a.name || ('ОКПО ' + a.okpo) }}</span>
              @if (running() || itemFor(a.okpo)) {
                <span class="rstate">{{ stateLabel(itemFor(a.okpo)) }}</span>
                @if (itemFor(a.okpo); as it) {
                  @if (it.state === 'ok') {
                    <nz-tag class="chip" nzColor="success"
                            nz-tooltip [nzTooltipTitle]="'срез ' + formation(it)">{{ it.rows }} ваг.</nz-tag>
                  } @else if (it.state === 'fail') {
                    <nz-tag class="chip" nzColor="error" nz-tooltip [nzTooltipTitle]="it.error">отказ</nz-tag>
                  }
                }
              } @else {
                <input nz-input type="password" autocomplete="off" nzSize="small"
                       [placeholder]="'пароль ' + a.login"
                       [ngModel]="pwdFor(a.okpo)"
                       (ngModelChange)="setPwd(a.okpo, $event)"
                       (keydown.enter)="run()" />
              }
            </div>
          }

          <div class="rfoot">
            @if (running()) {
              <span class="hint">{{ stageLabel() }} — окно можно закрыть, работа не прервётся</span>
            } @else if (lastItems().length) {
              <span class="hint">получено потоков: {{ state()?.ok }} из {{ lastItems().length }}</span>
            }
            <span class="spacer"></span>
            <button nz-button nzType="primary" nzSize="small"
                    [disabled]="!hasAnyPwd() || running()" [nzLoading]="running()" (click)="run()">
              Забрать из ЛК
            </button>
          </div>
        }

        <!-- Итог обновления дислокации: сводка либо честная причина, почему его нет. -->
        @if (state(); as st) {
          @if (st.process_error) {
            <nz-alert class="note" nzType="error" [nzMessage]="st.process_error" />
          } @else if (st.process_skip && !st.running) {
            <nz-alert class="note" nzType="warning" [nzMessage]="st.process_skip" />
          }
          <app-lk-process-result [res]="st.processed ?? null" />
        }
      </ng-container>
    </nz-modal>
  `,
  styles: [`
    .ttl { cursor: move; user-select: none; }
    .bar { display: flex; align-items: center; gap: var(--space-sm); margin-bottom: var(--space-sm); }
    .hint { color: var(--color-text-secondary); font-size: var(--font-size-sm); }
    .spacer { flex: 1 1 auto; }
    .muted { color: var(--color-text-muted); margin: 0; }
    .rrow { display: flex; align-items: center; gap: var(--space-sm); margin-bottom: 4px; }
    .rname { flex: 0 0 140px; font-size: var(--font-size-sm); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    .rstate { flex: 1 1 auto; font-size: var(--font-size-sm); color: var(--color-text-secondary); }
    .chip { margin: 0; white-space: nowrap; }
    .rfoot { display: flex; align-items: center; gap: var(--space-sm); margin-top: 6px;
             padding-bottom: var(--space-sm); border-bottom: 1px solid var(--color-border, #f0f0f0); }
    .note { margin-top: var(--space-md); }
  `],
})
export class LkRobotModalComponent implements OnInit, OnDestroy {
  private readonly api = inject(DislocationApiService);
  private readonly msg = inject(NzMessageService);

  readonly closed = output<void>();
  /** Снимок пересобран — родитель освежает статус-панель и счётчики. */
  readonly updated = output<void>();

  readonly accounts = signal<LKRobotAccount[]>([]);
  /** Введённые пароли: живут только в этом окне, на сервер уходят и там не сохраняются. */
  readonly pwd = signal<Record<number, string>>({});
  readonly state = signal<LKRobotState | null>(null);

  readonly running = computed(() => this.state()?.running ?? false);
  readonly lastItems = computed(() => this.state()?.items ?? []);
  readonly hasAnyPwd = computed(() => Object.values(this.pwd()).some((p) => !!p));

  /** Опрос состояния — только пока работа идёт. */
  private poll: ReturnType<typeof setInterval> | null = null;
  /** Сводка, о которой уже сообщили наверх: тост и обновление экрана — один раз. */
  private reportedAt = '';

  ngOnInit(): void {
    void this.loadAccounts();
    void this.load();
  }

  ngOnDestroy(): void {
    this.stopPoll();
  }

  setPwd(okpo: number, value: string): void {
    this.pwd.update((m) => ({ ...m, [okpo]: value }));
  }

  /** Введённый пароль потока (в шаблоне — без ?? , иначе NG8102). */
  pwdFor(okpo: number): string {
    return this.pwd()[okpo] ?? '';
  }

  itemFor(okpo: number): LKRobotItem | undefined {
    return this.lastItems().find((i) => i.okpo === okpo);
  }

  /** Список потоков для автовыгрузки. Робот выключен — окно честно об этом говорит. */
  private async loadAccounts(): Promise<void> {
    try {
      const res = await this.api.robotAccounts();
      this.accounts.set(res.accounts ?? []);
    } catch {
      this.accounts.set([]);
    }
  }

  /** Запуск: ответ приходит сразу, дальше следим за состоянием опросом. */
  async run(): Promise<void> {
    const accounts = Object.entries(this.pwd())
      .filter(([, password]) => !!password)
      .map(([okpo, password]) => ({ okpo: Number(okpo), password }));
    if (!accounts.length) return;

    try {
      this.apply(await this.api.robotRun(accounts));
      // Пароли не держим дольше запуска.
      this.pwd.set({});
      this.startPoll();
    } catch (err) {
      this.msg.error(apiErrorMessage(err));
      // Даже отказ запуска обновляет картину: вдруг забор уже идёт с другого места.
      void this.load();
    }
  }

  /** Состояние с сервера; в фоне (опрос) молча, по кнопке — с тостом об ошибке. */
  async load(quiet = false): Promise<void> {
    try {
      this.apply(await this.api.robotState());
    } catch (err) {
      if (!quiet) this.msg.error(apiErrorMessage(err));
    }
  }

  /**
   * Новое состояние в окно. Здесь же — единственное место, где мы сообщаем
   * наверх о пересобранной дислокации: по метке завершения, чтобы тост и
   * обновление главного экрана случились ровно один раз на запуск.
   */
  private apply(st: LKRobotState): void {
    this.state.set(st);
    if (st.running) {
      this.startPoll();
      return;
    }
    this.stopPoll();

    const stamp = st.finished_at ?? '';
    if (!stamp || stamp === this.reportedAt) return;
    this.reportedAt = stamp;
    if (st.processed) {
      this.msg.success(`Дислокация обновлена из ЛК: ${st.processed.count} ваг. (было ${st.processed.prev_snapshot})`);
      this.updated.emit();
    }
    for (const it of st.items.filter((i) => i.state === 'fail')) {
      this.msg.error(`${it.name || 'ОКПО ' + it.okpo}: ${it.error}`);
    }
  }

  private startPoll(): void {
    if (this.poll) return;
    this.poll = setInterval(() => void this.load(true), 2000);
  }

  private stopPoll(): void {
    if (this.poll) {
      clearInterval(this.poll);
      this.poll = null;
    }
  }

  /** Стадия запуска словами — она же подпись под кнопкой, пока работа идёт. */
  stageLabel(): string {
    switch (this.state()?.stage) {
      case 'fetch': return 'идёт забор из кабинета';
      case 'process': return 'обновляется дислокация';
      default: return 'работаем';
    }
  }

  /** Состояние одного потока словами. */
  stateLabel(it: LKRobotItem | undefined): string {
    switch (it?.state) {
      case 'wait': return 'ждёт очереди';
      case 'run': return 'заходим в кабинет…';
      case 'ok': return 'данные получены';
      case 'fail': return 'не получилось';
      default: return '';
    }
  }

  /** Метка среза кабинета «ДД.ММ ЧЧ:ММ» — по ней видно, насколько свежи данные. */
  formation(it: LKRobotItem): string {
    const ts = it.formation_ts;
    if (!ts) return 'неизвестен';
    const [date, time] = ts.split('T');
    const [, m, d] = date.split('-');
    return `${d}.${m} ${(time ?? '').slice(0, 5)}`;
  }
}
