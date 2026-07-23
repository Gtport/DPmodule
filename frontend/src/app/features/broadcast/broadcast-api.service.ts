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

/** Поезда одной ЖД-даты (готовые строки, формат gtport). */
export interface PlanFormDay {
  date: string; // yyyy-MM-dd
  trains: string[];
}

/** Карточка одного терминала «ЖД сутки». */
export interface PlanFormTerminal {
  terminal: string;
  color: string;
  lines: PlanFormLine[];
  days: PlanFormDay[];
}

/**
 * Клиент канала MAX. Бэкенд — тонкий релей: форму (текст/PNG) собирает фронт,
 * сервер разрешает адресатов по маршруту (форма × терминал) и шлёт. Ручки:
 * GET /max/chats, POST /max/broadcast/text|image.
 */
@Injectable({ providedIn: 'root' })
export class BroadcastApiService {
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
