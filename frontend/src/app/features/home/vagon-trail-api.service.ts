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

/** Визит станции — непрерывная серия операций на ней (first/last + все ops). */
export interface TrailVisit {
  stan_op: string;
  station: string;
  road: string;
  first: TrailOp;
  last: TrailOp;
  count: number;
  ops: TrailOp[];
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
