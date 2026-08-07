import { HttpClient } from '@angular/common/http';
import { Injectable, inject } from '@angular/core';
import { firstValueFrom } from 'rxjs';
import { environment } from '../../../environments/environment';

/**
 * API экрана «Карта»: группы «поезд на станции» с координатами (ключ trainKey —
 * тот же, что у «Поездов в движении»), вагоны группы по требованию, пометка
 * диспетчера (текст+цвет → info_1/info_2 снимка). Типы = контракт
 * service.MapDataDTO / MapWagonsDTO / MapMarkRequest.
 */

/** Маркер карты: агрегат группы «поезд на станции». */
export interface MapGroup {
  key: string;
  index: string;
  station_oper: string;
  doroga_oper: string;
  stan_nazn: string;
  lat: number | null; // null → группа в списке no_coords («не на карте»)
  lon: number | null;
  vagon_count: number;
  status: number | null;
  broshen: boolean;
  arrived: boolean;
  time_op: string | null;
  oper_s: string;
  plan_jd: string | null;
  prog_jd: string | null; // непустой = «ходовой», пустой = «отцепка»
  rasst: number | null;
  naznach: string; // терминал-большинство (фильтры/попап)
  naznach_list: string[]; // все терминалы группы → фильтр чипами
  color: string; // marka.color, если единогласна по вагонам; пусто → красим по статусу
  mark_text: string; // пометка диспетчера (info_1)
  mark_color: string; // пометка диспетчера (info_2)
  sub_groups: MapSubgroup[]; // состав поезда для попапа (без вагонов)
}

/** Подгруппа отправки в попапе («Состав (N)» gtport). */
export interface MapSubgroup {
  key: string;
  index_main: string;
  station_nach: string;
  gruzotpr: string;
  gruzpol_s: string;
  naznach: string;
  cargo_s: string;
  cargo_group: string;
  client?: string;
  vagon_count: number;
  ves: number;
  color: string; // marka.color — цветная полоса строки состава
}

/** Терминал реестра портов — чип фильтра с цветом (ports.color). */
export interface MapTarget {
  name: string;
  station: string;
  station_code: string;
  color: string;
}

export interface MapDataResponse {
  groups: MapGroup[];
  no_coords: MapGroup[];
  targets: MapTarget[];
  total: number;
  vagons: number;
}

/** Вагон группы (drill-down из попапа маркера). */
export interface MapWagon {
  id: string; // id рейса — для «Истории движения вагона»
  vagon: string;
  npp_vag: number | null;
  invoice: string;
  freight: string;
  ves: number | null;
  owner: string;
  status: number | null;
  gruzpol_s: string;
  naznach: string;
  station_nach: string; // станция отправления
  gruzotpr: string; // отправитель
  date_otpr: string | null; // дата отправления
  date_dostav: string | null; // нормативный срок доставки
}

export interface MapWagonsResponse {
  key: string;
  wagons: MapWagon[];
}

export interface MapMarkRequest {
  keys: string[];
  text: string;
  color: string;
}

export interface MapMarkResult {
  updated: number;
  selected: number;
  forecast_computed: number;
  prog_computed: number;
}

@Injectable({ providedIn: 'root' })
export class MapsApiService {
  private readonly http = inject(HttpClient);
  private readonly base = `${environment.apiBaseUrl}/v1/dislocation`;

  getMapData(): Promise<MapDataResponse> {
    return firstValueFrom(this.http.get<MapDataResponse>(`${this.base}/map`));
  }

  getWagons(key: string): Promise<MapWagonsResponse> {
    return firstValueFrom(
      this.http.get<MapWagonsResponse>(`${this.base}/map/wagons`, { params: { key } }),
    );
  }

  applyMark(req: MapMarkRequest): Promise<MapMarkResult> {
    return firstValueFrom(this.http.post<MapMarkResult>(`${this.base}/map/mark`, req));
  }
}
