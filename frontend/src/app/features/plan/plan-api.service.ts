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
}

/** Сетка загрузки плана: заголовок + строки. */
export interface PlanGrid {
  plan: Plan;
  nitki: PlanNitka[];
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

  upload(code: string, file: File): Promise<{ filename: string; nitki: number; matched: number; stamped: number; cleared: number }> {
    const form = new FormData();
    form.set('code', code);
    form.set('file', file);
    return firstValueFrom(this.http.post<{ filename: string; nitki: number; matched: number; stamped: number; cleared: number }>(`${this.base}/upload`, form));
  }
}
