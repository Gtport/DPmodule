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

  /** Правка выбранных вагонов истории (прибытие/отмена/выгрузка/назначение). */
  updateVagons(body: ArrivalsUpdate): Promise<ArrivalsUpdateResult> {
    return firstValueFrom(this.http.put<ArrivalsUpdateResult>(`${this.base}/arrivals/vagons`, body));
  }

  /** Кандидаты в прибывшие (статус 9, минус отклонённые) по терминалам. */
  getCandidates(naznach: string[]): Promise<CandidateGroup[]> {
    const params: Record<string, string> = {};
    if (naznach.length) params['naznach'] = naznach.join(',');
    return firstValueFrom(this.http.get<CandidateGroup[]>(`${this.base}/arrivals/candidates`, { params }));
  }

  /** Подтверждение прибытия: снимок получает статус 10 + date_prib, веха — в историю.
   *  index — правка индекса поезда оператором (пусто — оставить как в снимке). */
  confirmArrival(vagonIds: string[], datePrib: string, index = ''): Promise<ArrivalsUpdateResult> {
    return firstValueFrom(this.http.post<ArrivalsUpdateResult>(`${this.base}/arrivals/confirm`, {
      vagon_ids: vagonIds, date_prib: datePrib, index: index || undefined,
    }));
  }

  /** Отклонение кандидатов («скрыть до новых данных»). */
  dismissCandidates(vagonIds: string[]): Promise<ArrivalsUpdateResult> {
    return firstValueFrom(this.http.post<ArrivalsUpdateResult>(`${this.base}/arrivals/dismiss`, {
      vagon_ids: vagonIds,
    }));
  }

  /** Отмена прибытия: снимок 10→9 (вагон снова кандидат) + очистка вехи в истории. */
  cancelArrival(vagonIds: string[]): Promise<ArrivalsUpdateResult> {
    return firstValueFrom(this.http.post<ArrivalsUpdateResult>(`${this.base}/arrivals/cancel`, {
      vagon_ids: vagonIds,
    }));
  }
}

/** Поезд-кандидат в прибывшие (вагоны статуса 9 одного индекса). */
export interface CandidateGroup {
  key: string;
  index: string;
  stan_nazn: string;
  station_nach: string;
  time_op: string | null;
  vagon_count: number;
  sub_groups: ArrivalSubgroup[];
}

/** Тело правки: vagon_ids + только применяемые поля (времена — МСК без TZ). */
export interface ArrivalsUpdate {
  vagon_ids: string[];
  index_pp?: string;
  plan_jd?: string;
  date_prib?: string;
  date_vigr?: string;
  place_vigr?: string;
  frost?: number;
  naznach?: string;
}

export interface ArrivalsUpdateResult {
  updated: number;
  selected: number;
}
