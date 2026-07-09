/**
 * Бэкенд всегда отвечает на ошибку телом { error: string } (см.
 * internal/handler/response.go). HttpClient оборачивает такой ответ в
 * HttpErrorResponse — достаём человекочитаемое сообщение оттуда.
 */
export function apiErrorMessage(err: unknown): string {
  const body = (err as { error?: unknown } | undefined)?.error;
  if (typeof body === 'string') return body;
  if (body && typeof body === 'object' && 'error' in body) {
    const inner = (body as { error?: unknown }).error;
    if (typeof inner === 'string') return inner;
  }
  return 'Не удалось выполнить запрос';
}
