import { HttpClient } from '@angular/common/http';
import { Injectable, inject } from '@angular/core';
import { firstValueFrom } from 'rxjs';
import { environment } from '../../../environments/environment';

/**
 * Пропавший вагон: был в снимке, исчез из выгрузки в незавершённом рейсе
 * (статус 8, таблица status9). Последняя известная позиция. Времена — МСК naive.
 */
export interface MissingVagon {
  /** id рейса (= dislocation.id = vagon_history.id) — по нему открывается история движения. */
  id: string;
  vagon: string;
  index: string;
  station_oper: string;
  doroga_oper: string;
  oper_s: string;
  time_op: string | null;
  naznach: string;
  gruzpol_s: string;
  stan_nazn: string;
  cargo_s: string;
  ves: number | null;
  date_dostav: string | null;
  missing_since: string;
  days_missing: number;
}

/** Подгруппа пропавшего поезда (одно назначение/получатель), раскрывается до вагонов. */
export interface MissingSubgroup {
  key: string;
  index_main: string;
  station_nach: string;
  naznach: string;
  gruzpol_s: string;
  vagon_count: number;
  /** Готовая строка состава «(N)-783-Челутай АЭ» — формат подгрупп прибывших. */
  display: string;
  vagons: MissingVagon[];
}

/** Пропавший поезд (группа index|stan_nazn — как у кандидатов прибытия). */
export interface MissingGroup {
  key: string;
  index: string;
  stan_nazn: string;
  station_oper: string;
  doroga_oper: string;
  oper_s: string;
  /** Самая свежая операция группы — где состав видели в последний раз. */
  time_op: string | null;
  missing_since: string;
  days_missing: number;
  vagon_count: number;
  sub_groups: MissingSubgroup[];
}

/** Итог подтверждения: сколько строк реально обновлено из выбранных. */
export interface MissingConfirmResult {
  updated: number;
  selected: number;
}

/**
 * Донор перегруза (статус 6): у него приёмники забирают груз/назначение.
 * Поля те же, что у пропавшего, кроме давности: «донором с».
 */
export interface Status6Vagon {
  id: string;
  vagon: string;
  index: string;
  station_oper: string;
  doroga_oper: string;
  oper_s: string;
  time_op: string | null;
  naznach: string;
  gruzpol_s: string;
  stan_nazn: string;
  cargo_s: string;
  ves: number | null;
  date_dostav: string | null;
  since: string;
  days_donor: number;
}

@Injectable({ providedIn: 'root' })
export class MissingApiService {
  private readonly http = inject(HttpClient);
  private readonly base = `${environment.apiBaseUrl}/v1/dislocation`;

  /** Пропавшие агрегированно: поезд → подгруппа → вагоны, с фильтром по
   *  терминалам станции (карточка «Кандидаты на прибытие»; пусто — все). */
  getMissingGroups(naznach: string[] = []): Promise<MissingGroup[]> {
    const params: Record<string, string> = {};
    if (naznach.length) params['naznach'] = naznach.join(',');
    return firstValueFrom(this.http.get<MissingGroup[]>(`${this.base}/missing/groups`, { params }));
  }

  /**
   * «Подтвердить прибытие» пропавшего: веха в историю (с выгрузкой — статус 12)
   * и снятие из списка. Времена — строки без TZ (yyyy-MM-ddTHH:mm:00) в шкале
   * timeBase ('jd'|'msk' — пересчёт к шкалам хранения делает сервер);
   * место пустое — терминал назначения записи; index — правка индекса поезда.
   */
  confirmMissing(vagonIds: string[], datePrib: string, dateVigr = '', placeVigr = '',
                 index = '', timeBase = ''): Promise<MissingConfirmResult> {
    return firstValueFrom(this.http.post<MissingConfirmResult>(`${this.base}/missing/confirm`, {
      vagon_ids: vagonIds,
      date_prib: datePrib,
      date_vigr: dateVigr || undefined,
      place_vigr: placeVigr || undefined,
      index: index || undefined,
      time_base: timeBase || undefined,
    }));
  }

  /** Доноры перегруза (статус 6) — из RAM-кэша снимка. */
  getStatus6(): Promise<Status6Vagon[]> {
    return firstValueFrom(this.http.get<Status6Vagon[]>(`${this.base}/status6`));
  }
}
