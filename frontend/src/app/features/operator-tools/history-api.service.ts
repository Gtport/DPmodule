import { HttpClient } from '@angular/common/http';
import { Injectable, inject } from '@angular/core';
import { firstValueFrom } from 'rxjs';
import { environment } from '../../../environments/environment';
import { TrainsTarget } from '../trains/trains-api.service';

/**
 * API экрана «Работа с историческими данными» (перенос gtport
 * POST /history/universal). Поиск и Excel — POST: списки вагонов вставляются из
 * Excel сотнями номеров, в query string не влезут. Пагинация и сортировка —
 * серверные (в vagon_history ~100 тыс. записей — грузить всё нельзя).
 * Типы = контракт service.HistorySearch*.
 */

/** Фильтр запроса; все поля опциональны (пусто = фильтр выключен). */
export interface HistoryFilter {
  date_nach_d_from?: string; // yyyy-MM-dd
  date_nach_d_to?: string;
  date_prib_d_from?: string;
  date_prib_d_to?: string;
  date_vigr_d_from?: string;
  date_vigr_d_to?: string;
  gruzpol_s?: string[];
  naznach?: string[];
  place_vigr?: string[];
  not_unloaded?: boolean;
  only_overdue?: boolean; // просрочка доставки: delay > 0 (зафиксирована при прибытии)
  only_not_arrived?: boolean; // недоехавшие: рейс закрыт удалением из пропавших
  vagons?: string[];
  invoices?: string[];
  station_nach?: string[];
}

export interface HistorySort {
  by: string; // ключ колонки из белого списка сервера
  dir: 'asc' | 'desc';
}

export interface HistorySearchRequest {
  filter: HistoryFilter;
  sort: HistorySort;
  page: { limit: number; offset: number };
}

/** Строка таблицы: 26 колонок + id (ПКМ «История движения вагона»). */
export interface HistoryRow {
  id: string;
  vagon: string;
  invoice_main: string;
  invoice: string;
  index_main: string;
  index_pp: string;
  date_nach_d: string | null;
  station_nach: string;
  gruzotpr: string;
  gruzpol_s: string;
  naznach: string;
  cargo_s: string;
  ves: number | null;
  client: string;
  status: number | null;
  date_dostav: string | null;
  date_prib_d: string | null;
  plan_jd: string | null;
  delay: number | null;
  date_vigr: string | null;
  /** ЖД-сутки выгрузки — шкала фильтра date_vigr_d и учётных счётчиков. */
  date_vigr_d: string | null;
  place_vigr: string;
  frost: number | null;
  owner: string;
  freight: string; // марка груза (freight_exact_name)
  gtd_number: string;
  shipments: string;
  peregruz: string;
  /** Недоехавший: рейс закрыт вручную удалением из пропавших (пометка в «Статусе»). */
  not_arrived: boolean;
}

export interface HistorySearchResponse {
  rows: HistoryRow[];
  total: number;
  limit: number;
  offset: number;
  has_more: boolean;
}

/** Данные панели фильтров: терминалы реестра с цветами + станции погрузки. */
export interface HistoryMeta {
  targets: TrainsTarget[];
  stations: string[];
}

@Injectable({ providedIn: 'root' })
export class HistoryApiService {
  private readonly http = inject(HttpClient);
  private readonly base = `${environment.apiBaseUrl}/v1/history`;

  search(req: HistorySearchRequest): Promise<HistorySearchResponse> {
    return firstValueFrom(this.http.post<HistorySearchResponse>(`${this.base}/search`, req));
  }

  meta(): Promise<HistoryMeta> {
    return firstValueFrom(this.http.get<HistoryMeta>(`${this.base}/meta`));
  }

  /** Книга .xlsx по всему фильтру (page на сервере игнорируется). */
  excel(req: HistorySearchRequest): Promise<Blob> {
    return firstValueFrom(this.http.post(`${this.base}/excel`, req, { responseType: 'blob' }));
  }
}
