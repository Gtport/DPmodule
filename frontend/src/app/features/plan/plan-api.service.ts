import { HttpClient } from '@angular/common/http';
import { Injectable, inject } from '@angular/core';
import { firstValueFrom } from 'rxjs';
import { environment } from '../../../environments/environment';

/** Ячейка порта: метка столбца (терминал/груз из файла) + число вагонов. */
export interface PortCell {
  label: string;
  count: number;
  is_our: boolean;
}

/** Заголовок одной загрузки плана (история: несколько на станцию). */
export interface Plan {
  id: number;
  plan_code: string;
  source_file: string;
  loaded_at: string | null;
  nitki: number;
  matched: number;
  stamped: number;
}

/** Краткая карточка загрузки для списка выбора. */
export interface PlanSummary {
  id: number;
  plan_code: string;
  source_file: string;
  loaded_at: string | null;
  nitki: number;
  matched: number;
  stamped: number;
}

/** Одна строка плана — нитка поезда или служебная «Остаток на 18:00» (is_ostatok). */
export interface PlanNitka {
  plan_code: string;
  ord: number;
  index: string;
  index_pp: string;
  station_oper: string;
  plan_msk: string | null;
  plan_jd: string | null;
  fact_msk: string | null;
  otkl: string;
  wagons: number;
  activ: number;
  ports: PortCell[] | null;
  sostav: string;
  comment: string;
  matched: boolean;
  matched_wagons: number;
  is_ostatok: boolean;
  is_sf: boolean;
}

/** Сетка загрузки плана: заголовок + строки. */
export interface PlanGrid {
  plan: Plan;
  nitki: PlanNitka[];
}

/** Группа-кандидат вагонов для с.ф. (диалог выбора). */
export interface SFCandidate {
  key: string;     // уникальный идентификатор группы для выбора (id_disl не уникален!)
  id_disl: string;
  station: string;
  index: string;
  date: string;
  quantity: number;
  sostav: string;
  vagons: string[];
}

/** Одна с.ф.-нитка плана с её кандидатами. ports — столбцы терминалов из строки плана. */
export interface SFRow {
  ord: number;
  index_pp: string;
  plan_msk: string | null;
  ports: PortCell[] | null;
  candidates: SFCandidate[];
}

/** Проблемная обычная нитка: Activ>0, но матч не нашёл вагонов (вероятна опечатка в
 *  индексе, поезд ещё не в дислокации либо уже прибыл). Оператор вписывает индекс. */
export interface ProblemRow {
  ord: number;
  index_pp: string;
  plan_msk: string | null;
  activ: number;
  ports: PortCell[] | null;
}

/** Ответ prepare/revalidate: токен + с.ф.-строки + проблемные нитки + превью.
 *  sf/problems — на границе сети допускаем null (пустой набор), рендер это учитывает. */
export interface PreparePlanResult {
  token: string;
  plan_code: string;
  filename: string;
  sf: SFRow[] | null;
  problems: ProblemRow[] | null;
  nitki: number;
  matched: number;
}

/** Результат применения плана (upload/confirm). */
export interface PlanApplyResult {
  filename: string;
  nitki: number;
  matched: number;
  stamped: number;
  cleared: number;
}

/** Актуальность одной ветки дислокации (файл ЛК грузополучателя): d_attis/d_nmtp. */
export interface DislTermStatus {
  organisation: string;
  terminals: string[];
  formation_ts: string | null;
  age_minutes: number;
}

/** Актуальность снимка дислокации в целом. */
export interface DislStatus {
  source: string;               // способ обновления (lk/json)
  doc_ts: string | null;        // общая метка формирования (самая старая)
  updated_at: string | null;    // когда снимок пересобран
  actor: string;                // кто обновил
  age_minutes: number;          // возраст по doc_ts, мин
  terminals: DislTermStatus[];
}

/** Актуальность загрузки плана подвода станции. */
export interface PlanStatus {
  plan_code: string;
  loaded: boolean;
  doc_ts: string | null;        // дата плана из документа
  updated_at: string | null;    // когда загружен
  actor: string;
  filename: string;
  age_minutes: number;          // с момента загрузки, мин
}

/** Статус-панель: актуальность дислокации и планов. */
export interface SystemStatus {
  now: string;
  dislocation: DislStatus | null;
  plans: PlanStatus[];
}

/** Клиент подсистемы «план подвода» (история загрузок + таблица нитей). */
@Injectable({ providedIn: 'root' })
export class PlanApiService {
  private readonly http = inject(HttpClient);
  private readonly base = `${environment.apiBaseUrl}/v1/dislocation/plan`;

  /** Самая свежая загрузка станции. */
  getLatest(code: string): Promise<PlanGrid> {
    return firstValueFrom(this.http.get<PlanGrid>(`${this.base}/${code}`));
  }

  /** Конкретная загрузка из истории по id. */
  getById(code: string, id: number): Promise<PlanGrid> {
    return firstValueFrom(this.http.get<PlanGrid>(`${this.base}/${code}?id=${id}`));
  }

  /** Список загрузок станции (свежие первыми). */
  async listPlans(code: string): Promise<PlanSummary[]> {
    const res = await firstValueFrom(
      this.http.get<{ plans: PlanSummary[] }>(`${this.base}/${code}/history`),
    );
    return res.plans ?? [];
  }

  upload(code: string, file: File): Promise<PlanApplyResult> {
    const form = new FormData();
    form.set('code', code);
    form.set('file', file);
    return firstValueFrom(this.http.post<PlanApplyResult>(`${this.base}/upload`, form));
  }

  /** Фаза A: разбор плана + кандидаты с.ф. + проблемные нитки. Снимок не изменяется. */
  prepare(code: string, file: File): Promise<PreparePlanResult> {
    const form = new FormData();
    form.set('code', code);
    form.set('file', file);
    return firstValueFrom(this.http.post<PreparePlanResult>(`${this.base}/prepare`, form));
  }

  /** Сухой пересчёт превью с ручными правками индексов (overrides: ord → индекс 4-3-4).
   *  Снимок не трогаем, токен не расходуем — для итеративной коррекции перед confirm. */
  revalidate(token: string, overrides: Record<number, string>): Promise<PreparePlanResult> {
    return firstValueFrom(
      this.http.post<PreparePlanResult>(`${this.base}/revalidate`, { token, overrides }),
    );
  }

  /** Фаза B: применить план с правками индексов (overrides: ord → индекс) и выбранными
   *  группами с.ф. (selections: ord → id_disl[]). */
  confirm(
    token: string,
    overrides: Record<number, string>,
    selections: Record<number, string[]>,
  ): Promise<PlanApplyResult> {
    return firstValueFrom(
      this.http.post<PlanApplyResult>(`${this.base}/confirm`, { token, overrides, selections }),
    );
  }

  /** Heartbeat: продлить токен подготовки, пока открыт диалог с.ф. (204 — ок, 410 — истёк). */
  touch(token: string): Promise<void> {
    return firstValueFrom(this.http.post<void>(`${this.base}/touch`, { token }));
  }

  /** Статус-панель: актуальность дислокации (по терминалам) и планов подвода.
   *  Маршрут — /dislocation/status (НЕ под /plan), поэтому строим URL отдельно. */
  getStatus(): Promise<SystemStatus> {
    const url = `${environment.apiBaseUrl}/v1/dislocation/status`;
    return firstValueFrom(this.http.get<SystemStatus>(url));
  }
}
