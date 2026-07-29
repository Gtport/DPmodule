import { HttpClient } from '@angular/common/http';
import { Injectable, inject } from '@angular/core';
import { firstValueFrom } from 'rxjs';
import { environment } from '../../../environments/environment';

/** Линия выгрузки за сутки (подмножество CargoWorkLineDTO бэка). */
export interface VygruzkaLine {
  cargo_key: string;
  label: string;
  pc: number;
  ost_18: number;
  ost_st: number;
  prib: number;
  useful_formation: number;
  total_formation: number;
  downtime: string; // «Ч:ММ»
  plan: number;
  vigr_fact: number;
  vigr_stan: number;
  prim: string;
  ost: number;
  effectiv: number;
  perepokaz: number;
}

export interface VygruzkaLoadLine {
  cargo_key: string;
  label: string;
  load_fact: number;
  plan: number;
  ost: number;
}

/** Учётный лист одного дня (CargoWorkDayDTO бэка). */
export interface VygruzkaDay {
  date: string; // yyyy-MM-dd
  terminal: string;
  color: string;
  lines: VygruzkaLine[];
  load: VygruzkaLoadLine[];
}

export interface VygruzkaPeriod {
  from: string;
  to: string;
  terminal: string;
  color: string;
  days: VygruzkaDay[]; // по возрастанию даты
}

/** Клиент отчёта «Выгрузка за период» (данные «Грузовой работы»). */
@Injectable({ providedIn: 'root' })
export class VygruzkaApiService {
  private readonly http = inject(HttpClient);
  private readonly base = `${environment.apiBaseUrl}/v1/reports/vygruzka`;

  period(terminal: string, from: string, to: string): Promise<VygruzkaPeriod> {
    return firstValueFrom(this.http.get<VygruzkaPeriod>(this.base, { params: { terminal, from, to } }));
  }
}
