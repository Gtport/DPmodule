import { HttpClient } from '@angular/common/http';
import { Injectable, inject } from '@angular/core';
import { firstValueFrom } from 'rxjs';
import { environment } from '../../../environments/environment';

/**
 * Экран «Новый прогноз» (перенос gtport PrognozNew): сервер отдаёт сырьё одним
 * ответом, раскладку по датам-колонкам и итоги считает компонент.
 */

/** Подгруппа вагонов поезда: станция отправления × назначение × группа груза. */
export interface ForecastSubGroup {
  station_nach: string;
  date_nach: string | null; // дата погрузки (максимум по вагонам)
  vagon_count: number;
  cargo_group: string;
  naznach: string;
  color: string;
}

/** Едущий поезд с прогнозом прибытия (Stage 4). */
export interface ForecastTrainGroup {
  index: string;
  station_oper: string;
  status: number | null; // 5 — брошен (красная подсветка)
  prog_jd: string | null;
  sub_groups: ForecastSubGroup[];
}

/** Поезд, прибывший за сегодняшние сутки (vagon_history). */
export interface ForecastArrivedGroup {
  index_pp: string;
  date_prib: string | null;
  sub_groups: ForecastSubGroup[];
}

/** Линия выгрузки терминала: подтаблица экрана, норма и входящий остаток. */
export interface ForecastLine {
  terminal: string;
  cargo_key: string; // пусто — терминал считается одной таблицей
  label: string;
  pc: number; // норма выгрузки, ваг/сут (стартовое значение поля «Выгрузка»)
  ost: number; // «Остаток на 18:00»: вчерашний остаток линии из грузовой работы
}

export interface ForecastBoardDTO {
  groups: ForecastTrainGroup[];
  arrived: ForecastArrivedGroup[];
  lines: ForecastLine[];
}

@Injectable({ providedIn: 'root' })
export class ForecastApiService {
  private readonly http = inject(HttpClient);
  private readonly base = `${environment.apiBaseUrl}/v1/dislocation`;

  getBoard(): Promise<ForecastBoardDTO> {
    return firstValueFrom(this.http.get<ForecastBoardDTO>(`${this.base}/forecast/board`));
  }
}
