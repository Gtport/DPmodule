import { HttpClient, HttpParams } from '@angular/common/http';
import { Injectable, inject } from '@angular/core';
import { firstValueFrom } from 'rxjs';
import { environment } from '../../../environments/environment';

/** Вагон с истекшим сроком доставки (разворот группы). id — id рейса для истории движения. */
export interface OverdueVagon {
  id: string;
  vagon: string;
  index: string;
  station_oper: string;
  oper_s: string;
  time_op: string | null;
  status: number | null;
  cargo_s: string;
  ves: number | null;
  naznach: string;
  date_nach: string | null;
  date_dostav: string | null;
  delay: number;
}

/**
 * Группа просроченных вагонов одной накладной — единица претензии к перевозчику
 * (пени по ст. 97 УЖТ считаются от провозной платы по накладной). Пустой key —
 * вагоны без накладной («Без накладной» на экране).
 */
export interface OverdueGroup {
  key: string;
  invoice: string;
  invoice_main: string;
  station_nach: string;
  gruzotpr: string;
  stan_nazn: string;
  gruzpol_s: string;
  cargo_s: string;
  vagon_count: number;
  max_delay: number;
  date_dostav: string | null;
  vagons: OverdueVagon[];
}

@Injectable({ providedIn: 'root' })
export class OverdueApiService {
  private readonly http = inject(HttpClient);
  private readonly base = `${environment.apiBaseUrl}/v1`;

  /** Просроченные вагоны снимка группами по накладной, самые просроченные первыми. */
  getGroups(): Promise<OverdueGroup[]> {
    return firstValueFrom(this.http.get<OverdueGroup[]>(`${this.base}/dislocation/overdue`));
  }

  /**
   * «Отчёт для претензии»: книга .xlsx по vagon_history (delay > 0) за период
   * прибытия [from; to] (YYYY-MM-DD), опционально по одному терминалу.
   */
  claimExcel(from: string, to: string, terminal: string): Promise<Blob> {
    let params = new HttpParams().set('from', from).set('to', to);
    if (terminal) params = params.set('terminal', terminal);
    return firstValueFrom(
      this.http.get(`${this.base}/reports/overdue/excel`, { params, responseType: 'blob' }),
    );
  }
}
