import { HttpClient } from '@angular/common/http';
import { Injectable, inject } from '@angular/core';
import { firstValueFrom } from 'rxjs';
import { environment } from '../../../environments/environment';

/** Заголовок загруженного плана подвода (одна станция плана = один план). */
export interface Plan {
  plan_code: string;
  source_file: string;
  loaded_at: string | null;
  nitki: number;
  matched: number;
  stamped: number;
}

/** Одна нитка плана (строка расписания) — время как пришло с бэка, МСК naive, без конверсий. */
export interface PlanNitka {
  plan_code: string;
  ord: number;
  index: string;
  index_pp: string;
  plan_msk: string | null;
  plan_jd: string | null;
  fact_msk: string | null;
  otkl: string;
  wagons: number;
  activ: number;
  matched: boolean;
  matched_wagons: number;
}

/** Сетка загруженного плана: заголовок + нитки. */
export interface PlanGrid {
  plan: Plan;
  nitki: PlanNitka[];
}

/** Результат загрузки файла плана (разбор + матч + простановка PlanMsk в снимок). */
export interface PlanUploadResult {
  filename: string;
  plan_code: string;
  nitki: number;
  matched: number;
  stamped: number;
  cleared: number;
}

/** Клиент подсистемы «план подвода». Стиль — как в DislocationApiService/AuthService. */
@Injectable({ providedIn: 'root' })
export class PlanApiService {
  private readonly http = inject(HttpClient);
  private readonly base = `${environment.apiBaseUrl}/v1/dislocation/plan`;

  getPlan(code: string): Promise<PlanGrid> {
    return firstValueFrom(this.http.get<PlanGrid>(`${this.base}/${code}`));
  }

  upload(code: string, file: File): Promise<PlanUploadResult> {
    const form = new FormData();
    form.set('code', code);
    form.set('file', file);
    return firstValueFrom(this.http.post<PlanUploadResult>(`${this.base}/upload`, form));
  }
}
