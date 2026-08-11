import { HttpClient } from '@angular/common/http';
import { Injectable, inject } from '@angular/core';
import { firstValueFrom } from 'rxjs';
import { environment } from '../../../environments/environment';

/** Уведомление колокольчика (ответ GET /notifications). */
export interface AppNotification {
  id: number;
  /** info | warning | error | service — цвет/иконка в списке. */
  type: string;
  title: string;
  message: string;
  /** Deep-link: bros | arrivals | unmatched | admin_dict (пусто — без перехода). */
  action_component?: string;
  action_params?: Record<string, unknown>;
  created_at: string;
  is_read: boolean;
  read_at?: string;
}

/**
 * API внутренних уведомлений (перенос колокольчика gtport). Видимость режет
 * сервер по ролям claims — фронт просто показывает, что пришло.
 */
@Injectable({ providedIn: 'root' })
export class NotificationsApiService {
  private readonly http = inject(HttpClient);
  private readonly base = `${environment.apiBaseUrl}/v1/notifications`;

  /** Список уведомлений, новые сверху. */
  list(unreadOnly = false, limit = 50): Promise<AppNotification[]> {
    const params: Record<string, string> = { limit: String(limit) };
    if (unreadOnly) params['unread'] = 'true';
    return firstValueFrom(this.http.get<AppNotification[]>(this.base, { params }));
  }

  /** Число непрочитанных (бейдж; лёгкий опрос раз в минуту). */
  async unreadCount(): Promise<number> {
    const r = await firstValueFrom(
      this.http.get<{ count: number }>(`${this.base}/unread-count`));
    return r.count;
  }

  /** Отметить прочитанным (идемпотентно). */
  markRead(id: number): Promise<unknown> {
    return firstValueFrom(this.http.put(`${this.base}/${id}/read`, null));
  }

  /** Прочитать все видимые. */
  markAllRead(): Promise<unknown> {
    return firstValueFrom(this.http.put(`${this.base}/read-all`, null));
  }
}
