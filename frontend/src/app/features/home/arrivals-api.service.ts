import { HttpClient } from '@angular/common/http';
import { Injectable, inject } from '@angular/core';
import { firstValueFrom } from 'rxjs';
import { environment } from '../../../environments/environment';

/** Вагон подгруппы прибывшего поезда (разворот). */
export interface ArrivalVagon {
  id: string;
  vagon: string;
  shipments?: string;
  status?: number | null;
}

/** Подгруппа прибывшего поезда: display — готовая строка «(N)-783-Челутай АЭ». */
export interface ArrivalSubgroup {
  key: string;
  index_main: string;
  station_nach: string;
  naznach: string;
  gruzpol_s: string;
  sms_1?: string;
  vagon_count: number;
  display: string;
  vagons: ArrivalVagon[];
}

/** Прибывший поезд (группа index_pp + date_prib из vagon_history). */
export interface ArrivalGroup {
  key: string;
  index_pp: string;
  stan_nazn: string;
  date_prib_d: string | null;
  date_prib: string | null;
  plan_msk: string | null;
  plan_jd: string | null;
  otkl: string;
  vagon_count: number;
  sub_groups: ArrivalSubgroup[];
}

/** Цель-терминал с его станцией (реестр ports; раскладка домашней страницы). */
export interface TerminalTarget {
  name: string;
  station: string;
  station_code: string;
}

export interface ArrivalsResponse {
  from: string;
  to: string;
  groups: ArrivalGroup[];
  targets: TerminalTarget[];
  total: number;
}

@Injectable({ providedIn: 'root' })
export class ArrivalsApiService {
  private readonly http = inject(HttpClient);
  private readonly base = `${environment.apiBaseUrl}/v1/dislocation`;

  /** Прибывшие за период (пустые даты — вчера/сегодня по МСК на бэке). */
  getArrivals(naznach: string[], from = '', to = ''): Promise<ArrivalsResponse> {
    const params: Record<string, string> = {};
    if (naznach.length) params['naznach'] = naznach.join(',');
    if (from) params['from'] = from;
    if (to) params['to'] = to;
    return firstValueFrom(this.http.get<ArrivalsResponse>(`${this.base}/arrivals`, { params }));
  }

  /** Реестр терминалов с их станциями — раскладка половин домашней страницы. */
  getTerminals(): Promise<TerminalTarget[]> {
    return firstValueFrom(this.http.get<TerminalTarget[]>(`${this.base}/terminals`));
  }
}
