import { HttpClient } from '@angular/common/http';
import { Injectable, inject } from '@angular/core';
import { firstValueFrom } from 'rxjs';
import { environment } from '../../../environments/environment';

/** Один staged-файл ЛК в папке приёма (шаг между загрузкой и обработкой). */
export interface LKFileInfo {
  okpo: string;
  organisation: string;
  terminals: string[];
  formation_ts: string;
  age_minutes: number;
  filename: string;
}

/** Замечание контроля приёма: 'block' — обработка небезопасна, 'warning' — можно, но обратить внимание. */
export interface LKIssue {
  level: 'block' | 'warning';
  code: string;
  okpo?: string;
  message: string;
}

/** Сводка staged-файлов ЛК + результат контроля приёма (ready = можно обрабатывать). */
export interface LKStatus {
  co_arrival_group: string;
  files: LKFileInfo[];
  issues: LKIssue[];
  ready: boolean;
}

/** Результат сохранения одного файла (шаг 1). */
export interface LKUploadResult {
  okpo: string;
  organisation: string;
  terminals: string[];
  formation_ts: string;
  filename: string;
  replaced: boolean;
}

/** Отчёт обработки всех принятых файлов в снимок дислокации (шаг 2). */
export interface LKProcessResult {
  count: number;
  files: number;
  prev_snapshot: number;
  per_file: Record<string, number>;
  nazn_enriched: number;
  stations_not_found: number[];
  ops_not_found: number[];
  port_unresolved: number;
  port_disabled: number;
  status9_inserted: number;
  status9_removed: number;
  status8_missing: number;
  carry_matched: number;
  carry_new: number;
  carry_sticky: number;
  status6_donors: number;
  status6_matched: number;
  marka_candidates: number;
  marka_filled: number;
  marka_missed: number;
  naznach_override: number;
  forecast_computed: number;
  prog_computed: number;
  history_inserted: number;
  history_updated: number;
  status_dist: Record<string, number>;
}

/**
 * Клиент двухшагового приёма ЛК: загрузка файла(ов) → контроль приёма →
 * обработка в снимок дислокации. Стиль — как в AuthService: async/await +
 * firstValueFrom, без RxJS-подписок; ошибки не мапятся здесь — наверх летит
 * голый HttpErrorResponse, компонент сам решает, что показать.
 */
@Injectable({ providedIn: 'root' })
export class DislocationApiService {
  private readonly http = inject(HttpClient);
  private readonly base = `${environment.apiBaseUrl}/v1/dislocation/lk`;

  getStatus(): Promise<LKStatus> {
    return firstValueFrom(this.http.get<LKStatus>(`${this.base}/files`));
  }

  upload(file: File): Promise<LKUploadResult> {
    const form = new FormData();
    form.set('file', file);
    return firstValueFrom(this.http.post<LKUploadResult>(`${this.base}/upload`, form));
  }

  process(): Promise<LKProcessResult> {
    return firstValueFrom(this.http.post<LKProcessResult>(`${this.base}/process`, {}));
  }

  /** Ручной забор дислокации из АСУ (тот же конвейер, что крон). Маршрут — не под /lk. */
  asuPull(): Promise<LKProcessResult> {
    return firstValueFrom(
      this.http.post<LKProcessResult>(`${environment.apiBaseUrl}/v1/dislocation/asu/pull`, {}),
    );
  }
}
