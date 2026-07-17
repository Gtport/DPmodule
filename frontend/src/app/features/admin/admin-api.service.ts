import { Injectable, inject } from '@angular/core';
import { HttpClient } from '@angular/common/http';
import { firstValueFrom } from 'rxjs';
import { environment } from '../../../environments/environment';

/** Запись реестра редактируемых справочников (dpport.list_tables). */
export interface AdminTable {
  name: string;    // имя таблицы
  name_ru: string; // подпись для владельца
  pk: string;      // колонка-идентификатор строки
}

/** Колонка справочника — для грида и динамической формы. */
export interface AdminColumn {
  name: string;
  kind: 'number' | 'text' | 'boolean';
  required: boolean;
  pk: boolean;
}

/** Строка справочника в динамическом виде. */
export type AdminRow = Record<string, unknown>;

export interface AdminTableData {
  table: AdminTable;
  columns: AdminColumn[];
  rows: AdminRow[] | null;
}

/**
 * Клиент админ-редактора справочников: универсальный CRUD по таблицам реестра
 * list_tables (только роль administrator). Стиль — как остальные api-сервисы:
 * async/await + firstValueFrom, ошибки наверх голым HttpErrorResponse.
 */
@Injectable({ providedIn: 'root' })
export class AdminApiService {
  private readonly http = inject(HttpClient);
  private readonly base = `${environment.apiBaseUrl}/v1/admin/tables`;

  tables(): Promise<AdminTable[]> {
    return firstValueFrom(this.http.get<AdminTable[]>(this.base));
  }

  tableData(table: string): Promise<AdminTableData> {
    return firstValueFrom(this.http.get<AdminTableData>(`${this.base}/${table}`));
  }

  create(table: string, values: AdminRow): Promise<unknown> {
    return firstValueFrom(this.http.post(`${this.base}/${table}`, values));
  }

  update(table: string, id: string, values: AdminRow): Promise<unknown> {
    return firstValueFrom(this.http.put(`${this.base}/${table}/${encodeURIComponent(id)}`, values));
  }

  remove(table: string, id: string): Promise<unknown> {
    return firstValueFrom(this.http.delete(`${this.base}/${table}/${encodeURIComponent(id)}`));
  }
}
