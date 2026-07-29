import { HttpClient } from '@angular/common/http';
import { Injectable, inject } from '@angular/core';
import { firstValueFrom } from 'rxjs';
import { environment } from '../../../environments/environment';

/** Строка «Факта перестановок» (раскладка gtport RearrangementFact). */
export interface PerestanovkaFactRow {
  vagon: string;
  invoice_main?: string;
  invoice?: string;
  index_main?: string;
  index_pp?: string;
  date_nach_d?: string | null;
  station_nach?: string;
  gruzotpr?: string;
  gruzpol_s?: string;
  naznach?: string;
  cargo_s?: string;
  ves?: number | null;
  client?: string;
  date_dostav?: string | null;
  date_prib?: string | null;
  plan_jd?: string | null;
  delay?: number | null;
  date_vigr?: string | null;
  place_vigr?: string;
  frost?: number | null;
  owner?: string;
  marka?: string;
  gtd?: string;
  shipments?: string;
}

export interface PerestanovkaFactDTO {
  from: string;
  to: string;
  rows: PerestanovkaFactRow[];
  total: number;
}

/**
 * Клиент блока «Перестановки» страницы «Справки и отчёты»:
 * - excel — текущие перестановки на терминал книгой .xlsx (собирает сервер);
 * - fact — факт перестановок за период из vagon_history (Excel собирает фронт).
 */
@Injectable({ providedIn: 'root' })
export class PerestanovkaApiService {
  private readonly http = inject(HttpClient);
  private readonly base = `${environment.apiBaseUrl}/v1/reports/perestanovka`;

  excel(terminal: string): Promise<Blob> {
    return firstValueFrom(this.http.get(`${this.base}/excel`,
      { params: { terminal }, responseType: 'blob' }));
  }

  fact(from: string, to: string, by: 'prib' | 'vigr', terminal: string): Promise<PerestanovkaFactDTO> {
    const params: Record<string, string> = { from, to, by };
    if (terminal) params['terminal'] = terminal;
    return firstValueFrom(this.http.get<PerestanovkaFactDTO>(`${this.base}/fact`, { params }));
  }
}
