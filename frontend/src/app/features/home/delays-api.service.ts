import { HttpClient } from '@angular/common/http';
import { Injectable, inject } from '@angular/core';
import { firstValueFrom } from 'rxjs';
import { environment } from '../../../environments/environment';

/** Эпизод задержки вагона (vagon_delay): kind 4 — долгий простой, 5 — брошен. */
export interface DelayEpisode {
  id: number;
  vagon: string;
  date_nach_d: string | null;
  kind: number;
  group_key: string;
  index: string;
  index_main: string;
  station_code: string;
  station_name: string;
  doroga: string;
  date_from: string | null;
  /** null — вагон стоит до сих пор. */
  date_to: string | null;
  /** Полная длительность закрытого эпизода, часы. */
  hours: number | null;
  /** id строки рейса в vagon_history — открывает «Историю движения вагона»; пусто — рейса нет. */
  trip_id: string;
  /** Часть длительности, попавшая в период отчёта (вагоно-часы за период). */
  hours_in_period: number;
}

/** Агрегат по станции: вагоно-часы за период с разбивкой по виду. */
export interface DelayStationAgg {
  station_code: string;
  station_name: string;
  doroga: string;
  episodes: number;
  vagons: number;
  open_now: number;
  hours4: number;
  hours5: number;
  hours: number;
}

/** Отчёт «Простои за период». */
export interface DelayReport {
  records: DelayEpisode[];
  stations: DelayStationAgg[];
  total_hours: number;
  total_episodes: number;
  total_vagons: number;
  open_now: number;
}

/** Отчёт по задержкам вагонов (память vagon_delay), только чтение. */
@Injectable({ providedIn: 'root' })
export class DelaysApiService {
  private readonly http = inject(HttpClient);
  private readonly base = `${environment.apiBaseUrl}/v1/dislocation/delays`;

  /** Отчёт за период дат (обе включительно, YYYY-MM-DD, МСК). */
  report(from: string, to: string): Promise<DelayReport> {
    return firstValueFrom(this.http.get<DelayReport>(`${this.base}/report`, { params: { from, to } }));
  }
}
