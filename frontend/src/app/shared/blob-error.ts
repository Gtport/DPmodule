import { apiErrorMessage } from '../core/api/api-error';

/**
 * Текст ошибки скачивания: при responseType 'blob' HttpClient заворачивает и
 * JSON-тело ошибки в Blob — разбираем его, чтобы показать сообщение сервера
 * («нет вагонов на перестановку…»), а не безликое «Не удалось выполнить запрос».
 */
export async function blobErrorMessage(err: unknown): Promise<string> {
  const body = (err as { error?: unknown } | undefined)?.error;
  if (body instanceof Blob) {
    try {
      return apiErrorMessage({ error: JSON.parse(await body.text()) });
    } catch { /* не JSON — падаем в общий текст */ }
  }
  return apiErrorMessage(err);
}
