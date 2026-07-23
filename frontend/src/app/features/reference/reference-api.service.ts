import { HttpClient } from '@angular/common/http';
import { Injectable, inject } from '@angular/core';
import { firstValueFrom } from 'rxjs';
import { environment } from '../../../environments/environment';

/** Чат MAX для выбора адресатов (chat_id сервер не отдаёт — маршрут решает он сам). */
export interface MaxChat {
  name: string;
  description: string;
  is_active: boolean;
}

/** Исход рассылки по чатам маршрута: куда ушло, а куда нет и почему. */
export interface BroadcastResult {
  chats: number;
  sent: string[];
  failed: Record<string, string>;
}

/** Тип формы рассылки — совпадает с max_route.report на бэке. */
export type MaxReport = 'spravki' | 'oper' | 'plan';

/** Линия учёта карточки: факт вчера + прогноз сегодня. */
export interface PlanFormLine {
  cargo_key: string;
  label: string;
  ost_18: number; prib: number; useful_y: number; total_y: number; vigr_fact: number; ost_y: number;
  ost_today: number; useful_today: number; total_today: number; downtime_today: string;
}

/** Поезд формы: готовая строка (формат gtport) + обе даты суток. */
export interface PlanFormTrain {
  display: string;
  date_jd: string;  // ЖД-сутки (18:00→18:00)
  date_msk: string; // грузовые (МСК) сутки
  time_msk: string; // HH:MM — сортировка в ГР-режиме
}

/** Карточка одного терминала. Trains отсортированы по ЖД-суткам и позиции внутри. */
export interface PlanFormTerminal {
  terminal: string;
  color: string;
  lines: PlanFormLine[];
  trains: PlanFormTrain[];
}

/** Поезда одного дня (раскладка на стороне интерфейса: ЖД либо ГР). */
export interface PlanFormDay {
  date: string;
  trains: string[];
}

/** Раскладка поездов по суткам: 'jd' — ЖД (порядок как пришёл), 'msk' — грузовые. */
export function groupTrains(trains: PlanFormTrain[], mode: 'jd' | 'msk'): PlanFormDay[] {
  const by = new Map<string, PlanFormTrain[]>();
  for (const t of trains) {
    const key = mode === 'jd' ? t.date_jd : t.date_msk;
    const list = by.get(key);
    if (list) list.push(t); else by.set(key, [t]);
  }
  return [...by.keys()].sort().map((date) => {
    const list = by.get(date)!;
    // ЖД-порядок уже задан бэком (отсечка 18:00); в ГР-сутках — по времени МСК.
    if (mode === 'msk') list.sort((a, b) => a.time_msk.localeCompare(b.time_msk));
    return { date, trains: list.map((t) => t.display) };
  });
}

/**
 * Клиент канала MAX. Бэкенд — тонкий релей: форму (текст/PNG) собирает фронт,
 * сервер разрешает адресатов по маршруту (форма × терминал) и шлёт. Ручки:
 * GET /max/chats, POST /max/broadcast/text|image.
 */
@Injectable({ providedIn: 'root' })
export class ReferenceApiService {
  private readonly http = inject(HttpClient);
  private readonly base = `${environment.apiBaseUrl}/v1/max`;

  /** Справочник чатов (для статуса/выбора; отправку адресует сервер по маршруту). */
  chats(): Promise<MaxChat[]> {
    return firstValueFrom(this.http.get<MaxChat[]>(`${this.base}/chats`));
  }

  /** Форма «План подвода» по терминалам на дату (вчера факт + сегодня прогноз + поезда). */
  planForm(date: string): Promise<PlanFormTerminal[]> {
    const base = `${environment.apiBaseUrl}/v1/dislocation`;
    return firstValueFrom(this.http.get<PlanFormTerminal[]>(`${base}/plan-form`, { params: { date } }));
  }

  /** Рассылка текстовой формы по маршруту (форма × терминал → чаты). */
  sendText(report: MaxReport, terminal: string, text: string): Promise<BroadcastResult> {
    return firstValueFrom(
      this.http.post<BroadcastResult>(`${this.base}/broadcast/text`, { report, terminal, text }),
    );
  }

  /** Рассылка картинки формы (готовый PNG) по маршруту. */
  sendImage(report: MaxReport, terminal: string, image: Blob, filename: string, caption: string): Promise<BroadcastResult> {
    const form = new FormData();
    form.append('report', report);
    form.append('terminal', terminal);
    form.append('caption', caption);
    form.append('image', image, filename);
    return firstValueFrom(this.http.post<BroadcastResult>(`${this.base}/broadcast/image`, form));
  }
}
