import { HttpClient } from '@angular/common/http';
import { Injectable, inject } from '@angular/core';
import { firstValueFrom } from 'rxjs';
import { environment } from '../../../environments/environment';

/** Отрезок расчётных суток: выгрузка поезда/остатка либо простой порта. */
export interface CargoWorkOperation {
  start_time: string;
  end_time: string;
  operation: string;
  wagons: number;
  duration: string;
  train_name: string;
}

/** Ожидание поезда: прибыл, но фронт был занят предыдущим. */
export interface CargoWorkWait {
  train_name: string;
  arrival_time: string;
  start_time: string;
  wait_duration: string;
  wait_reason: string;
}

/** Расчёт суток линии (снимок): сколько терминал МОГ переработать. */
export interface CargoWorkAnalytics {
  useful_formation: number;
  total_formation: number;
  downtime: string;
  operations: CargoWorkOperation[];
  waits: CargoWorkWait[];
}

/**
 * Колонка таблицы выгрузки — линия учёта терминала (уголь/металл/чугун либо
 * одна строка «всего»). Состав колонок задаёт справочник port_cargo_line,
 * поэтому фронт их не знает заранее и рисует по ответу.
 *
 * Что откуда: ost_18/ost_st/prib/vigr_stan/образование/downtime — авто-слой
 * (пересчёт), plan/vigr_fact/prim — вводит диспетчер, ost/effectiv/perepokaz —
 * считает сервер (на клиенте их не дублируем: в gtport формулы разъезжались).
 */
export interface CargoWorkLine {
  cargo_key: string;
  label: string;
  pc: number;

  ost_18: number;
  ost_st: number;
  prib: number;
  useful_formation: number;
  total_formation: number;
  downtime: string;

  plan: number;
  vigr_fact: number;
  vigr_stan: number;
  prim: string;

  ost: number;
  effectiv: number;
  perepokaz: number;

  analytics?: CargoWorkAnalytics;
}

/** Строка таблицы погрузки (целиком ручная). */
export interface CargoWorkLoad {
  cargo_key: string;
  label: string;
  load_fact: number;
  plan: number;
  ost: number;
}

/** Учётный лист терминала за сутки. */
export interface CargoWorkDay {
  date: string;
  terminal: string;
  color: string;
  lines: CargoWorkLine[];
  load: CargoWorkLoad[];
}

/** Правки диспетчера: только ручные поля, по ключу линии. */
export interface CargoWorkManual {
  lines?: Record<string, { plan?: number; vigr_fact?: number; prim?: string }>;
  load?: Record<string, { load_fact?: number; plan?: number; ost?: number }>;
}

@Injectable({ providedIn: 'root' })
export class CargoWorkApiService {
  private readonly http = inject(HttpClient);
  private readonly base = `${environment.apiBaseUrl}/v1/cargo-work`;

  /** Учётный лист суток. Терминал — имя из реестра ports (АЭ/УТ-1/ГУТ-2). */
  getDay(date: string, terminal: string): Promise<CargoWorkDay> {
    return firstValueFrom(
      this.http.get<CargoWorkDay>(`${this.base}/${date}/${encodeURIComponent(terminal)}`),
    );
  }

  /** Сохранить ручные поля; сервер вернёт лист с пересчитанными производными. */
  save(date: string, terminal: string, manual: CargoWorkManual): Promise<CargoWorkDay> {
    return firstValueFrom(
      this.http.put<CargoWorkDay>(`${this.base}/${date}/${encodeURIComponent(terminal)}`, manual),
    );
  }

  /** Пересчитать авто-слой (история дополнилась) — ручные поля сохранятся. */
  recalc(date: string, terminal: string): Promise<CargoWorkDay> {
    return firstValueFrom(
      this.http.post<CargoWorkDay>(
        `${this.base}/${date}/${encodeURIComponent(terminal)}/recalc`, {},
      ),
    );
  }

  /** Удалить учёт суток по терминалу. */
  remove(date: string, terminal: string): Promise<void> {
    return firstValueFrom(
      this.http.delete<void>(`${this.base}/${date}/${encodeURIComponent(terminal)}`),
    );
  }
}
