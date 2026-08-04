import { HttpClient } from '@angular/common/http';
import { Injectable, inject } from '@angular/core';
import { firstValueFrom } from 'rxjs';
import { environment } from '../../../../environments/environment';

/**
 * Вкладка «Прогноз прибытия/выгрузки» (перенос страницы «Прогноз GT» gtport).
 * Симуляция выгрузки считается НА СЕРВЕРЕ (пакет unloadsim с golden-тестами) —
 * фронт только рисует; каждый пересчёт (смена даты/скорости/режима) — POST.
 */

/** Линия выгрузки терминала (поток симуляции). */
export interface GtLine {
  terminal: string;
  cargo_key: string; // пусто — все грузы терминала одним потоком
  label: string;
  plan_speed: number;
  norm_speed: number;
}

export interface GtTerminal {
  name: string;
  color: string;
  lines: GtLine[];
}

/** Режим вкладки: причальная станция с терминалами. */
export interface GtStation {
  code: string;
  terminals: GtTerminal[];
}

export interface GtContext {
  stations: GtStation[];
}

export interface GtSubGroup {
  key: string;
  station_nach: string;
  date_nach: string | null;
  vagon_count: number;
  cargo_group: string;
  naznach: string;
  color: string;
  index_main: string;
  is_universal: boolean;
}

export interface GtTrain {
  index: string;
  station_oper: string;
  status: string; // номер статуса; прибывший — 'history'
  is_arrived: boolean;
  plan_jd: string | null;
  prog_jd: string | null;
  rasch_jd: string | null;
  rasch_msk: string | null;
  mistake: number | null;
  to_go: number | null;
  vagon_count: number;
  sub_groups: GtSubGroup[];
}

/** Блок диаграммы Ганта (выгрузка / остаток / простой). */
export interface GtOperation {
  train_index: string;
  train_name: string;
  station_nach?: string;
  index_main?: string;
  gruzpol_s?: string;
  orig_index?: string;
  start_calc: string;
  end_calc: string;
  start_jd: string;
  end_jd: string;
  wagons: number;
  total_wagons: number;
  color: string;
  is_remainder: boolean;
  is_carried_over: boolean;
  is_partial: boolean;
  wait_min: number;
  original_arrival_jd?: string;
}

export interface GtCarried {
  index: string;
  wagons: number;
}

export interface GtDay {
  date: string; // YYYY-MM-DD (расчётные = ЖД-сутки)
  plan_speed: number;
  norm_speed: number;
  incoming_total: number;
  arrival: number;
  total_formation: number;
  useful_formation: number;
  unloaded: number;
  remaining: number;
  total_wait_min: number;
  carried_over: GtCarried[];
  operations: GtOperation[];
}

/** Поток выгрузки — одна диаграмма Ганта. */
export interface GtFlow {
  terminal: string;
  cargo_key: string;
  label: string;
  color: string;
  initial_remainder: number;
  days: GtDay[];
}

export interface GtSimulateRequest {
  station: string;
  start_date: string; // YYYY-MM-DD
  days: number;
  use_norm: boolean;
  /** «терминал|груз» → дата → ваг/сут (правки скорости на конкретные сутки). */
  speed_overrides: Record<string, Record<string, number>>;
}

export interface GtSimulateResponse {
  trains: GtTrain[];
  flows: GtFlow[];
  max_train_wagons: number;
}

@Injectable({ providedIn: 'root' })
export class GtForecastApiService {
  private readonly http = inject(HttpClient);
  private readonly base = `${environment.apiBaseUrl}/v1/dislocation/gt-forecast`;

  getContext(): Promise<GtContext> {
    return firstValueFrom(this.http.get<GtContext>(`${this.base}/context`));
  }

  simulate(req: GtSimulateRequest): Promise<GtSimulateResponse> {
    return firstValueFrom(this.http.post<GtSimulateResponse>(`${this.base}/simulate`, req));
  }
}
