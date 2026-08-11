import { HttpClient } from '@angular/common/http';
import { Injectable, inject } from '@angular/core';
import { firstValueFrom } from 'rxjs';
import { environment } from '../../../environments/environment';

/**
 * Вагон в долгостое: прибыл на станцию назначения и стоит там дольше порога.
 * `id` — id рейса, по нему открывается «История движения вагона».
 */
export interface LongStandVagon {
  id: string;
  vagon: string;
  index: string;
  station_oper: string;
  doroga_oper: string;
  oper_s: string;
  time_op: string | null;
  naznach: string;
  station_nach: string;
  gruzotpr: string;
  cargo_s: string;
  ves: number | null;
  status: number | null;
  /** «гружён» (статус 10) либо «выгружен» (12): под выгрузкой стоит или уже не убран. */
  state: string;
  /** Момент прибытия — от него считается стоянка. */
  since: string | null;
  hours: number;
  days: number;
}

/** Ответ ручки: список + действующий порог (им подписан заголовок модалки). */
export interface LongStandResponse {
  threshold_hours: number;
  vagons: LongStandVagon[];
}

@Injectable({ providedIn: 'root' })
export class LongStandApiService {
  private readonly http = inject(HttpClient);
  private readonly base = `${environment.apiBaseUrl}/v1`;

  /** Вагоны в долгостое, дольше стоящие первыми. */
  getList(): Promise<LongStandResponse> {
    return firstValueFrom(this.http.get<LongStandResponse>(`${this.base}/dislocation/long-stand`));
  }
}
