import { HttpClient } from '@angular/common/http';
import { Injectable, inject } from '@angular/core';
import { firstValueFrom } from 'rxjs';
import { environment } from '../../../environments/environment';

/** Брошенный поезд (снимок bros). Даты — «YYYY-MM-DDTHH:MM:SS» без TZ. */
export interface BrosRecord {
  id: string;
  id_index: string;
  index_0: string;
  index_1: string;
  station_br: string;
  doroga_br: string;
  date_br: string | null;
  gruzpol_s: string;
  date_pod: string | null;
  date_pod_fact: string | null;
  prog_0: string | null;
  prog_1: string | null;
  to_go: number | null;
  plan: string | null;
  plan_history: string;
  status_br: boolean;
  reason: string;
  comment: string;
  sostav: string;
  vagon_count: number;
}

/** Строка отчёта: бросок + разбивка суток простоя по типам причин. */
export interface BrosReportRow extends BrosRecord {
  days_total: number;
  days_code05: number;          // платное размещение (заявка)
  days_code01_agreed: number;   // согласованный простой (письмо)
  days_code01_notagreed: number; // несогласованный (ответственность РЖД)
  days_other: number;
}

/** Код бросания (классификатор РЖД). */
export interface BrosReasonCode {
  code: string;
  description: string;
}

/** Запись журнала броска. */
export interface BrosJournalEntry {
  id: number;
  bros_id: string;
  date: string | null;
  reason: string;
  comment: string;
  zayavka_nomer: string | null;
  zayavka_date: string | null;
  date_pod: string | null;
  reason_text: string | null;
  is_agreed: boolean | null;
  plan_pod: string | null;
  created_at: string | null;
  created_by: string;
}

/** Тело создания записи журнала (даты строками YYYY-MM-DD). */
export interface BrosJournalCreate {
  bros_id: string;
  reason: string;
  comment?: string;
  zayavka_nomer?: string;
  zayavka_date?: string;
  date_pod?: string;
  reason_text?: string;
  is_agreed?: boolean | null;
  plan_pod?: string;
}

@Injectable({ providedIn: 'root' })
export class BrosApiService {
  private readonly http = inject(HttpClient);
  private readonly base = `${environment.apiBaseUrl}/v1/dislocation/bros`;

  /** Активные броски (status_br=true), новые первыми. */
  async getActive(): Promise<BrosRecord[]> {
    const r = await firstValueFrom(
      this.http.get<{ records: BrosRecord[] }>(`${this.base}/active`),
    );
    return r.records ?? [];
  }

  /** Броски за период (отчёт): фильтр по терминалу и датам (YYYY-MM-DD). */
  async filter(start: string, end: string, gruzpol = ''): Promise<BrosRecord[]> {
    const params: Record<string, string> = { start_date: start, end_date: end, limit: '10000' };
    if (gruzpol) params['gruzpol_s'] = gruzpol;
    const r = await firstValueFrom(
      this.http.get<{ records: BrosRecord[] }>(`${this.base}/filter`, { params }),
    );
    return r.records ?? [];
  }

  /** Отчёт за период с разбивкой суток по кодам (агрегация журнала на бэке). */
  async report(start: string, end: string, gruzpol = ''): Promise<BrosReportRow[]> {
    const params: Record<string, string> = { start_date: start, end_date: end };
    if (gruzpol) params['gruzpol_s'] = gruzpol;
    const r = await firstValueFrom(
      this.http.get<{ records: BrosReportRow[] }>(`${this.base}/report`, { params }),
    );
    return r.records ?? [];
  }

  /** Поиск броска по индексу (подстрока, по всей базе; мин. 3 символа). */
  async search(q: string): Promise<BrosRecord[]> {
    const r = await firstValueFrom(
      this.http.get<{ records: BrosRecord[] }>(`${this.base}/search`, { params: { q } }),
    );
    return r.records ?? [];
  }

  /** Справочник кодов бросания. */
  async getReasonCodes(): Promise<BrosReasonCode[]> {
    const r = await firstValueFrom(
      this.http.get<{ codes: BrosReasonCode[] }>(`${this.base}/reason-codes`),
    );
    return r.codes ?? [];
  }

  /** История журнала броска (новые первыми). */
  async getJournal(brosId: string): Promise<BrosJournalEntry[]> {
    const r = await firstValueFrom(
      this.http.get<{ entries: BrosJournalEntry[] }>(`${this.base}/journal`, {
        params: { bros_id: brosId },
      }),
    );
    return r.entries ?? [];
  }

  /** Создать (или перезаписать за сегодня) запись журнала. */
  async createJournal(body: BrosJournalCreate): Promise<BrosJournalEntry> {
    const r = await firstValueFrom(
      this.http.post<{ entry: BrosJournalEntry }>(`${this.base}/journal`, body),
    );
    return r.entry;
  }

  /** Массовая фиксация активных на сегодня (перед рассылкой в MAX). */
  bulkSave(): Promise<{ result: { total: number; saved: number; failed: number } }> {
    return firstValueFrom(
      this.http.post<{ result: { total: number; saved: number; failed: number } }>(
        `${this.base}/journal/bulk-save`,
        {},
      ),
    );
  }
}
