import { HttpClient } from '@angular/common/http';
import { Injectable, inject } from '@angular/core';
import { firstValueFrom } from 'rxjs';
import { environment } from '../../../environments/environment';

/** Поезд на экране «Прогнозы»: состав + прогнозные поля Stage 3/4. Времена — МСК naive. */
export interface ForecastTrain {
  id_disl: string;
  index: string;
  naznach: string;
  gruzpol_s: string;
  sms_2: string;
  stan_nazn: string;
  vagon_count: number;
  ves: number;
  has_plan: boolean;
  plan_msk: string | null;
  rasch_msk: string | null;
  prog_msk: string | null;
  prog_jd: string | null;
  mistake: number | null;
}

@Injectable({ providedIn: 'root' })
export class ForecastApiService {
  private readonly http = inject(HttpClient);
  private readonly base = `${environment.apiBaseUrl}/v1/dislocation`;

  getForecast(): Promise<ForecastTrain[]> {
    return firstValueFrom(this.http.get<ForecastTrain[]>(`${this.base}/forecast`));
  }
}
