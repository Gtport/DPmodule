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
  gruzpol_s: string;
  naznach: string;
}

/** Второй уровень группировки (поля своего режима). */
export interface RearrSubGroup {
  key: string;
  index_main?: string;
  index?: string;
  station_oper: string;
  station_nach?: string;
  gruzpol_s: string;
  naznach: string;
  rasst_stan_nazn: number | null;
  status: number | null;
  vagon_count: number;
  vagons: RearrVagon[];
}

/** Первый уровень группировки (обе вкладки и оба режима). */
export interface RearrGroup {
  key: string;
  index_main?: string;
  index?: string;
  station_nach?: string;
  station_oper?: string;
  stan_nazn: string;
  stan_nazn_code: string;
  gruzpol_s?: string;
  naznach?: string;
  pereadr_port?: string;
  status?: number | null;
  available: boolean;
  vagon_count: number;
  sub_groups: RearrSubGroup[];
}

/** Цель перестановки/переадресации: терминал и его станция (реестр портов). */
export interface RearrTarget {
  name: string;
  station: string;
  /** 4-значный код станции терминала — правила «своя/чужая станция» по кодам. */
  station_code: string;
}

export interface RearrGroups {
  group_by: string;
  groups: RearrGroup[];
  targets: RearrTarget[];
  total: number;
}

export interface RearrApplyResult {
  updated: number;
  selected: number;
  forecast_computed: number;
  prog_computed: number;
}

/** Пара станций справочника naznach_station (панель станций). */
export interface NaznachStationRow {
  dest_station: string;
  origin_station: string;
  naznach: string;
  univers: boolean;
  enabled: boolean;
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

  applyRearrangement(vagonIds: string[], newNaznach: string, byGruzpol = false): Promise<RearrApplyResult> {
    return firstValueFrom(
      this.http.post<RearrApplyResult>(`${this.base}/rearrangement/apply`, {
        vagon_ids: vagonIds,
        new_naznach: newNaznach,
        by_gruzpol: byGruzpol,
      }),
    );
  }

  getStations(): Promise<NaznachStationRow[]> {
    return firstValueFrom(this.http.get<NaznachStationRow[]>(`${this.base}/rearrangement/stations`));
  }

  updateStationNaznach(destStation: string, originStation: string, naznach: string): Promise<unknown> {
    return firstValueFrom(
      this.http.patch(`${this.base}/rearrangement/stations`, {
        dest_station: destStation,
        origin_station: originStation,
        naznach,
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
