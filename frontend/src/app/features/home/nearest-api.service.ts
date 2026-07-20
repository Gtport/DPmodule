import { HttpClient } from '@angular/common/http';
import { Injectable, inject } from '@angular/core';
import { firstValueFrom } from 'rxjs';
import { environment } from '../../../environments/environment';

/** Вагон подгруппы подходящего поезда (натурный лист / Excel / СМС). */
export interface NearestVagon {
  id: string;
  vagon: string;
  npp_vag: number | null;
  invoice: string;
  cargo_s: string;
  ves: number | null;
  owner: string;
  naznach: string;
  gruzpol_s: string;
  sms_1?: string;
}

/** Подгруппа подходящего поезда. */
export interface NearestSubgroup {
  key: string;
  index_main: string;
  station_nach: string;
  naznach: string;
  gruzpol_s: string;
  sms_1?: string;
  vagon_count: number;
  display: string;
  vagons: NearestVagon[];
}

/** Подходящий поезд: лучшее время прибытия (план → прогноз → расчёт) и состав. */
export interface NearestTrain {
  key: string;
  index: string;
  stan_nazn: string;
  station_oper: string;
  doroga_oper: string;
  rasst: number | null;
  time_jd: string | null;
  time_msk: string | null;
  has_plan: boolean;
  broshen: boolean;
  vagon_count: number;
  ves: number;
  sub_groups: NearestSubgroup[];
}

@Injectable({ providedIn: 'root' })
export class NearestApiService {
  private readonly http = inject(HttpClient);
  private readonly base = `${environment.apiBaseUrl}/v1/dislocation`;

  /** Ближайшие поезда в подходе по терминалам станции. */
  getNearest(naznach: string[], limit = 50): Promise<NearestTrain[]> {
    const params: Record<string, string> = { limit: String(limit) };
    if (naznach.length) params['naznach'] = naznach.join(',');
    return firstValueFrom(this.http.get<NearestTrain[]>(`${this.base}/nearest`, { params }));
  }
}
