import { Injectable, inject, signal } from '@angular/core';
import { HttpClient } from '@angular/common/http';
import { NzMessageService } from 'ng-zorro-antd/message';
import { firstValueFrom } from 'rxjs';
import { environment } from '../../../environments/environment';
import { apiErrorMessage } from '../../core/api/api-error';
import { StaleTracker } from '../../shared/stale';

/** Поезд «бесплановых в подходе» (сигнал: движется без плана ближе порога). */
export interface UnplannedTrain {
  index: string;
  station_oper: string;
  rasst: number | null;
  vagon_count: number;
  sostav: string[];
  vagons: string[];
}

/** Строка «Оперативки»: терминал и суточные счётчики. */
export interface OperativkaRow {
  terminal: string;
  station: string;
  station_code: string;
  pogr_yesterday: number; // погружено в адрес терминала (без перегрузов)
  prib_yesterday: number;
  vigr_yesterday: number;
  pogr_today: number;
  prib_today: number;
  vigr_today: number;
  not_unloaded: number;
}

export interface Operativka {
  yesterday: string;
  today: string;
  rows: OperativkaRow[];
  unplanned: UnplannedTrain[];
}

/**
 * Данные «Оперативки» (`GET /dislocation/operativka`) — общие для двух карточек
 * главной: «Прибытие/выгрузка по терминалам» и «Без плана в подходе». Ответ один
 * на обе, поэтому запрос живёт здесь, а не в карточке: кто первый подключился —
 * тот заводит минутный опрос, последний отключившийся его гасит. Так карточки
 * можно ставить в любые места раскладки без дублирования запросов.
 */
@Injectable({ providedIn: 'root' })
export class OperativkaApiService {
  private readonly http = inject(HttpClient);
  private readonly msg = inject(NzMessageService);
  private readonly url = `${environment.apiBaseUrl}/v1/dislocation/operativka`;

  readonly loading = signal(false);
  readonly data = signal<Operativka | null>(null);
  /** Свежесть общего опроса: бейдж «данные N мин» в карточке «Прибытие/выгрузка». */
  readonly stale = new StaleTracker();

  private consumers = 0;
  private timer: ReturnType<typeof setInterval> | null = null;
  private inflight: Promise<void> | null = null;

  /** Карточка появилась на экране (ngOnInit). */
  attach(): void {
    if (++this.consumers > 1) return;
    void this.load(this.data() === null);
    this.timer = setInterval(() => void this.load(), 60_000);
  }

  /** Карточка ушла с экрана (ngOnDestroy). */
  detach(): void {
    if (--this.consumers > 0) return;
    this.consumers = 0;
    if (this.timer) clearInterval(this.timer);
    this.timer = null;
  }

  /** Перечитать. Параллельные вызовы (две карточки, крон) склеиваются в один запрос. */
  load(initial = false): Promise<void> {
    if (this.inflight) return this.inflight;
    if (initial) this.loading.set(true);
    this.inflight = (async () => {
      try {
        this.data.set(await firstValueFrom(this.http.get<Operativka>(this.url)));
        this.stale.ok();
      } catch (err) {
        this.stale.fail();
        if (initial) this.msg.error(apiErrorMessage(err));
      } finally {
        if (initial) this.loading.set(false);
        this.inflight = null;
      }
    })();
    return this.inflight;
  }

  /** «Скрыть» бесплановых (указание оператора) — появятся снова при новой смене станции. */
  async dismissUnplanned(u: UnplannedTrain): Promise<void> {
    await firstValueFrom(this.http.post(`${this.url}/unplanned/dismiss`, { vagons: u.vagons }));
    await this.load();
  }
}
