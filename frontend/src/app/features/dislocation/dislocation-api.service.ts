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

  /** Аккаунты ЛК для автовыгрузки: по одному на поток (порт). Логины — из таблицы lk_account. */
  robotAccounts(): Promise<{ accounts: LKRobotAccount[] }> {
    return firstValueFrom(this.http.get<{ accounts: LKRobotAccount[] }>(`${this.base}/robot/accounts`));
  }

  /**
   * Запуск робота ЛК: пароли вводит диспетчер, на сервере они не сохраняются.
   * Ответ приходит СРАЗУ (202) — забор идёт в фоне, ход смотрим через robotState().
   * Поэтому запуск не зависит от таймаутов nginx/ingress перед бэкендом.
   */
  robotRun(accounts: { okpo: number; password: string }[]): Promise<LKRobotState> {
    return firstValueFrom(this.http.post<LKRobotState>(`${this.base}/robot/run`, { accounts }));
  }

  /** Состояние забора: прогресс по потокам, приём и итог обновления одним ответом. */
  robotState(): Promise<LKRobotState> {
    return firstValueFrom(this.http.get<LKRobotState>(`${this.base}/robot/state`));
  }

  /** «Обновить справочники»: перезагрузка словарей в RAM + пересчёт снимка (после правки marka/cargo). */
  reloadDirectories(): Promise<DictReloadResult> {
    return firstValueFrom(
      this.http.post<DictReloadResult>(`${environment.apiBaseUrl}/v1/dislocation/directories/reload`, {}),
    );
  }
}

/** Аккаунт ЛК РЖД: один поток (порт) — один кабинет со своим логином. */
export interface LKRobotAccount {
  okpo: number;
  login: string;
  name: string;
}

/**
 * Один поток в ходе запуска: state — где он сейчас (wait ждёт очереди, run идёт,
 * ok файл принят, fail отвалился с причиной в error).
 */
export interface LKRobotItem {
  okpo: number;
  name: string;
  state: 'wait' | 'run' | 'ok' | 'fail';
  organisation?: string;
  filename?: string;
  rows?: number;
  error?: string;
}

/**
 * Состояние забора из ЛК целиком. Запуск фоновый: ручка `run` отвечает сразу,
 * а модалка опрашивает это состояние — прогресс живёт на сервере и переживает
 * закрытие окна. stage: idle (запусков не было) → fetch → process → done.
 */
export interface LKRobotState {
  running: boolean;
  stage: 'idle' | 'fetch' | 'process' | 'done';
  started_at?: string;
  finished_at?: string;
  actor?: string;
  items: LKRobotItem[];
  ok: number;
  failed: number;
  /** Итог обновления дислокации: сводка, либо причина, почему его не было. */
  processed?: LKProcessResult;
  process_skip?: string;
  process_error?: string;
  /** Приём как он есть сейчас — тот же список файлов и замечаний, что у ручной загрузки. */
  files: LKStatus;
}

/** Отчёт «Обновить справочники»: что пересчитано в снимке после перезагрузки словарей. */
export interface DictReloadResult {
  count: number;             // вагонов в снимке
  refreshed: number;         // атрибутированные, обновлены правкой словаря
  filled: number;            // были пустые — заполнены marka
  filled_by_train: number;   // заполнены наследованием по составу
  still_empty: number;       // остались без атрибуции
  forecast_computed: number; // пересчитан ход (Stage 3)
  prog_computed: number;     // пересчитан прогноз порта (Stage 4)
}
