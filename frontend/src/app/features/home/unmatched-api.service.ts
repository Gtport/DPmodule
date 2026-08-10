import { HttpClient } from '@angular/common/http';
import { Injectable, inject } from '@angular/core';
import { firstValueFrom } from 'rxjs';
import { environment } from '../../../environments/environment';

/** Вагон без бизнес-атрибуции (разворот группы). id — id рейса для истории движения. */
export interface UnmatchedVagon {
  id: string;
  vagon: string;
  index: string;
  station_oper: string;
  oper_s: string;
  time_op: string | null;
  status: number | null;
  code_cargo: string;
  cargo_s: string;
  ves: number | null;
  gruzpol_s: string;
  naznach: string;
  date_nach: string | null;
}

/** Подсказка предзаполнения: атрибуция того же ОКПО с другой станции/группы. */
export interface UnmatchedSuggest {
  gruzotpr: string;
  client: string;
  sms_1: string;
  sms_3: string;
  color: string;
}

/**
 * Группа несматченных вагонов — одна комбинация ключа матчинга marka
 * (ОКПО, станция отправления, группа груза; сырые строки потока).
 * reason: bad_okpo | bad_station | no_cargo_group | no_marka.
 */
export interface UnmatchedGroup {
  key: string;
  okpo: string;
  station_kod: string;
  station_nach: string;
  cargo_group: string;
  cargo_s: string;
  vagon_count: number;
  reason: string;
  okpo_in_marka: boolean;
  station_in_marka: boolean;
  group_in_marka: boolean;
  can_add_to_marka: boolean;
  suggest?: UnmatchedSuggest;
  vagons: UnmatchedVagon[];
}

/** Назначение атрибуции комбинации (ключ группы дословно из списка). */
export interface UnmatchedAssignRequest {
  okpo: string;
  station_kod: string;
  cargo_group: string;
  gruzotpr: string;
  client?: string;
  sms_1?: string;
  sms_3?: string;
  color?: string;
  add_to_marka?: boolean;
}

export interface UnmatchedAssignResult {
  updated: number;
  history_filled: number;
  marka_saved: boolean;
}

@Injectable({ providedIn: 'root' })
export class UnmatchedApiService {
  private readonly http = inject(HttpClient);
  private readonly base = `${environment.apiBaseUrl}/v1/dislocation/unmatched`;

  /** Несматченные с marka вагоны снимка группами по ключу, крупные первыми. */
  getGroups(): Promise<UnmatchedGroup[]> {
    return firstValueFrom(this.http.get<UnmatchedGroup[]>(this.base));
  }

  /** Назначить атрибуцию комбинации (опц. записать в словарь marka). Гейт — senior/admin. */
  assign(req: UnmatchedAssignRequest): Promise<UnmatchedAssignResult> {
    return firstValueFrom(this.http.post<UnmatchedAssignResult>(`${this.base}/assign`, req));
  }
}
