import { HttpClient } from '@angular/common/http';
import { Injectable, inject } from '@angular/core';
import { firstValueFrom } from 'rxjs';
import { environment } from '../../../environments/environment';

/** Колонка шапки формы (три уровня, как в файле порта). */
export interface NmtpColumnHead {
  /** nmtp_column.id — цель ручного переноса поезда. */
  id?: number;
  group: string;
  station: string;
  mark: string;
}

/** Строка-поезд формы. counts — по колонкам, последний элемент — «прочее». */
export interface NmtpTrainRow {
  index: string;
  station_oper: string;
  date_nach: string | null;
  note?: string;
  control_vagon: string;
  prog: string | null;
  planned: boolean;
  date_bros?: string | null;
  counts: number[];
  total: number;
  /** Только на клиенте: строка добавлена руками в модалке (сервер поле игнорирует). */
  custom?: boolean;
}

export interface NmtpSection {
  label: string;
  near: boolean;
  /** Причальная станция (не дорога): пустая на экране прячется, дорога — нет. */
  is_station?: boolean;
  /** null у пустых секций старого бэка (Go nil-слайс) — читать через `?? []`. */
  rows: NmtpTrainRow[] | null;
  total: number;
}

export interface NmtpClientTons {
  client: string;
  tons: number;
}

/** «Подход вагонов» по форме порта (НМТП) — контракт GET /reports/nmtp. */
export interface NmtpReport {
  terminal: string;
  columns: NmtpColumnHead[];
  has_other: boolean;
  sections: NmtpSection[];
  abandoned: NmtpSection[];
  col_counts: number[];
  col_tons: number[];
  total_vagons: number;
  total_tons: number;
  trains_active: number;
  trains_abandoned: number;
  unload_forecast: number;
  norm: number;
  client_tons: NmtpClientTons[];
}

/** Режим отбора: '' — получатель ИЛИ назначение; 'naznach' — скрыть перестановки. */
export type NmtpMode = '' | 'naznach';

@Injectable({ providedIn: 'root' })
export class NmtpApiService {
  private readonly http = inject(HttpClient);
  private readonly base = `${environment.apiBaseUrl}/v1/reports/nmtp`;

  /** Терминалы с настроенной раскладкой (кнопки карточки). */
  terminals(): Promise<string[]> {
    return firstValueFrom(this.http.get<string[]>(`${this.base}/terminals`));
  }

  /** Данные формы для экранной модалки. */
  report(terminal: string, mode: NmtpMode): Promise<NmtpReport> {
    const params: Record<string, string> = { terminal };
    if (mode) params['mode'] = mode;
    return firstValueFrom(this.http.get<NmtpReport>(this.base, { params }));
  }

  /** Книга .xlsx (сервер собирает) — в том же режиме, что на экране. */
  excel(terminal: string, mode: NmtpMode): Promise<Blob> {
    const params: Record<string, string> = { terminal };
    if (mode) params['mode'] = mode;
    return firstValueFrom(this.http.get(`${this.base}/excel`, { params, responseType: 'blob' }));
  }

  /** Книга .xlsx из ПРАВЛЕНОГО на экране отчёта — правки нигде не хранятся. */
  excelEdited(report: NmtpReport): Promise<Blob> {
    return firstValueFrom(this.http.post(`${this.base}/excel`, report, { responseType: 'blob' }));
  }

  /**
   * Перенос поезда в колонку: привязка по номерам вагонов (nmtp_vagon_column),
   * переживает переформирование. column_id = 0 — снять. from_column_id сужает
   * перенос до одной группы состава (id колонки, -1 — «прочее», 0 — весь состав).
   */
  move(req: {
    terminal: string; index: string; station_oper: string; prog: string;
    column_id: number; from_column_id: number;
  }): Promise<{ vagons: number }> {
    return firstValueFrom(this.http.post<{ vagons: number }>(`${this.base}/move`, req));
  }
}
