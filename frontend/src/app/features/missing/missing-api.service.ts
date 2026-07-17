import { HttpClient } from '@angular/common/http';
import { Injectable, inject } from '@angular/core';
import { firstValueFrom } from 'rxjs';
import { environment } from '../../../environments/environment';

/**
 * Пропавший вагон: был в снимке, исчез из выгрузки в незавершённом рейсе
 * (статус 8, таблица status9). Последняя известная позиция. Времена — МСК naive.
 */
export interface MissingVagon {
  vagon: string;
  index: string;
  station_oper: string;
  doroga_oper: string;
  oper_s: string;
  time_op: string | null;
  naznach: string;
  gruzpol_s: string;
  stan_nazn: string;
  cargo_s: string;
  ves: number | null;
  date_dostav: string | null;
  missing_since: string;
  days_missing: number;
}

@Injectable({ providedIn: 'root' })
export class MissingApiService {
  private readonly http = inject(HttpClient);
  private readonly base = `${environment.apiBaseUrl}/v1/dislocation`;

  getMissing(): Promise<MissingVagon[]> {
    return firstValueFrom(this.http.get<MissingVagon[]>(`${this.base}/missing`));
  }
}
