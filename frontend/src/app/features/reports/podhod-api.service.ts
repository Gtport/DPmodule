import { HttpClient } from '@angular/common/http';
import { Injectable, inject } from '@angular/core';
import { firstValueFrom } from 'rxjs';
import { environment } from '../../../environments/environment';

/** Подгруппа поезда: партия одного груза/отправителя (контракт gtport). */
export interface PodhodSubgroup {
  station_nach: string;
  date_nach: string | null; // самая частая дата погрузки подгруппы
  gruzotpr: string;
  vagon_count: number;
  total_weight: number;
  sprav_1: string;
  sprav_2: string; // первый вагон подгруппы
  sprav_3: string;
  prim_1: string; // «был CCC» при смене индекса
  prim_2: string; // переадресация / перестановка
  prim_3: string; // prim_1 + prim_2 (колонка «Примечание»)
  prim_4: string; // цветовая метка вагона (#RRGGBB)
}

/** Поезд (группа L1) с подгруппами; n — порядковый номер по прогнозу. */
export interface PodhodItem {
  n: number;
  index: string;
  plan_msk: string | null;
  station_oper: string;
  doroga_oper: string;
  oper_s: string;
  prog_msk: string | null;
  subgroups: PodhodSubgroup[];
}

/** Ответ отчёта; clients — все клиенты терминала (до фильтра), для мультиселекта. */
export interface PodhodReport {
  items: PodhodItem[];
  total: number;
  clients: string[];
}

/** Пресет отчёта — клиентский вариант карточки («Марис»), таблица report_preset. */
export interface ReportPreset {
  id: number;
  report: string;
  name: string;
  clients: string; // список клиентов через '|'
  sort_order: number;
  enabled: boolean;
}

/** Клиент отчётов страницы «Справки и отчёты» («Подход», «Повагонка»). */
@Injectable({ providedIn: 'root' })
export class PodhodApiService {
  private readonly http = inject(HttpClient);
  private readonly base = `${environment.apiBaseUrl}/v1/reports/podhod`;

  /** Отчёт по терминалу; clients — фильтр клиентов через '|' (пусто — все). */
  report(terminal: string, clients = ''): Promise<PodhodReport> {
    const params: Record<string, string> = { terminal };
    if (clients) params['clients'] = clients;
    return firstValueFrom(this.http.get<PodhodReport>(this.base, { params }));
  }

  /** Пресеты формы (карточки «Подход {имя}»). */
  presets(): Promise<ReportPreset[]> {
    return firstValueFrom(this.http.get<ReportPreset[]>(`${this.base}/presets`));
  }

  /** «Повагонка»: книга .xlsx со снимком (сервер собирает; пусто — весь снимок). */
  vagonkaExcel(terminal = ''): Promise<Blob> {
    const params: Record<string, string> = {};
    if (terminal) params['terminal'] = terminal;
    return firstValueFrom(this.http.get(`${environment.apiBaseUrl}/v1/reports/vagonka/excel`,
      { params, responseType: 'blob' }));
  }

  /** Терминалы с настроенной раскладкой НМТП-отчёта (кнопки карточки). */
  nmtpTerminals(): Promise<string[]> {
    return firstValueFrom(this.http.get<string[]>(`${environment.apiBaseUrl}/v1/reports/nmtp/terminals`));
  }

  /** «Подход вагонов» по форме порта (НМТП): книга .xlsx (сервер собирает). */
  nmtpExcel(terminal: string): Promise<Blob> {
    return firstValueFrom(this.http.get(`${environment.apiBaseUrl}/v1/reports/nmtp/excel`,
      { params: { terminal }, responseType: 'blob' }));
  }
}
