import { HttpClient } from '@angular/common/http';
import { Injectable, inject } from '@angular/core';
import { firstValueFrom } from 'rxjs';
import { environment } from '../../../environments/environment';

/** Операция продвижения (запрос 601) с именами из справочников. */
export interface TrailOp {
  date_op: string;
  kop_vmd: string;
  oper: string;
  oper_s: string;
  /** Индекс поезда, нормализованный: ХХХХ-ХХХ-ХХХХ либо «Б/И». */
  index: string;
}

/** Эпизод задержки рейса (vagon_delay): 4 — долгий простой, 5 — брошен. */
export interface TrailDelay {
  kind: number;
  station_code: string;
  /** Имя станции стоянки из снимка (полезно, когда визита в трейле нет). */
  station_name: string;
  date_from: string;
  /** Пусто — вагон стоит до сих пор. */
  date_to: string;
  hours: number;
}

/** Визит станции — непрерывная серия операций на ней (first/last + все ops). */
export interface TrailVisit {
  stan_op: string;
  station: string;
  road: string;
  first: TrailOp;
  last: TrailOp;
  count: number;
  ops: TrailOp[];
  /** Визит был задержкой (простой/бросание); нет поля — обычный визит. */
  delay?: TrailDelay;
}

/** История движения вагона по рейсу: период полученной истории + визиты. */
export interface VagonTrail {
  id: string;
  vagon: string;
  date_nach: string;
  terminal: string;
  from: string;
  to: string;
  count: number;
  visits: TrailVisit[];
  /** Все эпизоды задержек рейса (и не попавшие в сохранённый трейл тоже). */
  delays: TrailDelay[];
  /** Длительность рейса в часах: погрузка → прибытие (или «сейчас»). */
  trip_hours: number;
  /** Суммарные задержки рейса, часы. */
  delay_hours: number;
}

/**
 * История движения вагона: рейс адресуется id строки истории прибывших
 * (vagon_history) — вагон мог уже выбыть из снимка, история остаётся.
 */
@Injectable({ providedIn: 'root' })
export class VagonTrailApiService {
  private readonly http = inject(HttpClient);
  private readonly base = `${environment.apiBaseUrl}/v1/dislocation/vagons/trail`;

  /** Сохранённый трейл из базы (без обращения к АСУ). */
  get(id: string): Promise<VagonTrail> {
    return firstValueFrom(this.http.get<VagonTrail>(this.base, { params: { id } }));
  }

  /** Запрос к АСУ (601): дата погрузки −1 … сегодня, трейл перезаписывается. */
  pull(id: string): Promise<VagonTrail> {
    return firstValueFrom(this.http.post<VagonTrail>(`${this.base}/pull`, null, { params: { id } }));
  }
}
