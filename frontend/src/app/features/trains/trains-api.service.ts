import { HttpClient } from '@angular/common/http';
import { Injectable, inject } from '@angular/core';
import { firstValueFrom } from 'rxjs';
import { environment } from '../../../environments/environment';

/**
 * API экрана «Поезда в движении» (перенос gtport OperatorToolsDislocation).
 * Одна ручка: весь снимок деревом «поезд → подгруппа → вагоны», все фильтры
 * и поиски — на клиенте (как в gtport). Типы = контракт service.TrainsDTO.
 */

/** Вагон подгруппы. */
export interface TrainVagon {
  id: string; // id рейса — для «Истории движения вагона» по ПКМ
  vagon: string;
  npp_vag: number | null;
  invoice: string;
  freight: string; // марка груза (freight_exact_name, пусто — cargo_s)
  ves: number | null;
  owner: string; // собственник/оператор (наше поле; в gtport была rod_vag_uch)
  rod_vag: string;
  status: number | null;
  date_nach: string | null;
  date_dostav: string | null;
}

/** Подгруппа отправки: станция погрузки / получатель / груз. */
export interface TrainSubgroup {
  key: string;
  index_main: string;
  station_nach: string;
  gruzotpr: string;
  gruzpol_s: string;
  naznach: string;
  cargo_s: string;
  cargo_group: string;
  client?: string;
  shipments?: string;
  vagon_count: number;
  ves: number;
  vagons: TrainVagon[];
}

/** Строка-поезд (агрегат по вагонам). */
export interface Train {
  key: string;
  index: string;
  station_oper: string;
  doroga_oper: string;
  stan_nazn: string;
  rasst: number | null;
  status: number | null;
  broshen: boolean;
  arrived: boolean;
  has_plan: boolean;
  time_op: string | null;
  oper_s: string;
  plan_jd: string | null;
  rasch_jd: string | null;
  prog_jd: string | null; // непустой = «ходовой», пустой = «отцепка»
  prost_dn: number | null;
  prost_ch: number | null;
  vagon_count: number;
  ves: number;
  sub_groups: TrainSubgroup[];
}

/** Терминал реестра портов — чип фильтра с цветом (ports.color). */
export interface TrainsTarget {
  name: string;
  station: string;
  station_code: string;
  color: string;
}

export interface TrainsResponse {
  trains: Train[];
  targets: TrainsTarget[];
  total: number;
}

@Injectable({ providedIn: 'root' })
export class TrainsApiService {
  private readonly http = inject(HttpClient);
  private readonly base = `${environment.apiBaseUrl}/v1/dislocation`;

  getTrains(): Promise<TrainsResponse> {
    return firstValueFrom(this.http.get<TrainsResponse>(`${this.base}/trains`));
  }
}
