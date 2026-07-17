import { HttpClient } from '@angular/common/http';
import { Injectable, inject } from '@angular/core';
import { firstValueFrom } from 'rxjs';
import { environment } from '../../../environments/environment';

/** Вагон в подгруппе (детализация выбора). */
export interface RearrVagon {
  id: string;
  vagon: string;
  npp_vag: number | null;
  invoice: string;
  naznach: string;
}

/** Второй уровень группировки. */
export interface RearrSubGroup {
  key: string;
  label: string;
  naznach: string;
  vagon_count: number;
  vagons: RearrVagon[];
}

/** Первый уровень группировки (обе вкладки). */
export interface RearrGroup {
  key: string;
  title: string;
  subtitle: string;
  naznach: string;
  available: boolean;
  vagon_count: number;
  sub_groups: RearrSubGroup[];
}

export interface RearrGroups {
  group_by: string;
  groups: RearrGroup[];
  targets: string[];
  total: number;
}

export interface RearrApplyResult {
  updated: number;
  forecast_computed: number;
  prog_computed: number;
}

@Injectable({ providedIn: 'root' })
export class RearrangeApiService {
  private readonly http = inject(HttpClient);
  private readonly base = `${environment.apiBaseUrl}/v1/dislocation`;

  getRearrangementGroups(groupBy: string): Promise<RearrGroups> {
    return firstValueFrom(
      this.http.get<RearrGroups>(`${this.base}/rearrangement/groups`, { params: { group_by: groupBy } }),
    );
  }

  applyRearrangement(vagonIds: string[], newNaznach: string): Promise<RearrApplyResult> {
    return firstValueFrom(
      this.http.post<RearrApplyResult>(`${this.base}/rearrangement/apply`, {
        vagon_ids: vagonIds,
        new_naznach: newNaznach,
      }),
    );
  }

  getRedirectionGroups(): Promise<RearrGroups> {
    return firstValueFrom(this.http.get<RearrGroups>(`${this.base}/redirection/groups`));
  }

  applyRedirection(vagonIds: string[], kind: 'own' | 'ext' | 'cancel', target: string): Promise<RearrApplyResult> {
    return firstValueFrom(
      this.http.post<RearrApplyResult>(`${this.base}/redirection/apply`, {
        vagon_ids: vagonIds,
        kind,
        target,
      }),
    );
  }
}
