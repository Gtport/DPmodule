import { HttpClient } from '@angular/common/http';
import { Injectable, inject } from '@angular/core';
import { firstValueFrom } from 'rxjs';
import { environment } from '../../../environments/environment';

/** Дневной агрегат погрузки (строка ответа /reports/loading, контракт бэка). */
export interface LoadingDailyRow {
  day: string; // yyyy-MM-ddT00:00:00 (сутки погрузки, date_nach_d)
  gruzpol_s: string;
  sms_1: string;
  station_nach: string;
  client: string;
  cargo_group: string;
  vagon_count: number;
  total_weight: number;
}

export interface LoadingReportDTO {
  from: string;
  to: string;
  rows: LoadingDailyRow[];
}

/** Клиент отчёта «Погрузка» (страница «Справки и отчёты»). */
@Injectable({ providedIn: 'root' })
export class LoadingApiService {
  private readonly http = inject(HttpClient);
  private readonly base = `${environment.apiBaseUrl}/v1/reports/loading`;

  /** Дневные агрегаты погрузки за период [from; to], даты yyyy-MM-dd. */
  report(from: string, to: string): Promise<LoadingReportDTO> {
    return firstValueFrom(this.http.get<LoadingReportDTO>(this.base, { params: { from, to } }));
  }
}
