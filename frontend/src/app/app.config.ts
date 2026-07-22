import {
  ApplicationConfig,
  LOCALE_ID,
  provideAppInitializer,
  provideZoneChangeDetection,
  inject,
} from '@angular/core';
import { provideRouter } from '@angular/router';
import { provideHttpClient, withInterceptors, withFetch } from '@angular/common/http';
import { provideAnimationsAsync } from '@angular/platform-browser/animations/async';
import { registerLocaleData } from '@angular/common';
import localeRu from '@angular/common/locales/ru';

import { provideNzI18n, ru_RU } from 'ng-zorro-antd/i18n';
import { provideNzIcons } from 'ng-zorro-antd/icon';
import {
  MenuOutline, UserOutline, LockOutline, LogoutOutline, SettingOutline, AppstoreOutline,
  DashboardOutline, CalendarOutline, ScheduleOutline, DeploymentUnitOutline,
  FileDoneOutline, EnvironmentOutline, DatabaseOutline, ContainerOutline, MailOutline,
  SwapOutline, GlobalOutline, BarChartOutline, LineChartOutline, UploadOutline, InboxOutline,
  ReloadOutline, PrinterOutline, InfoCircleOutline, SyncOutline, // тулбар плана подвода
  CloudDownloadOutline, // забор из АСУ на экране дислокации
  QuestionCircleOutline, // сайдбар: «Пропавшие вагоны»
  DownOutline, RightOutline, // дерево групп на «Перестановках»
  CheckOutline, // «Применить» на «Перестановках»
  BookOutline, CopyOutline, DeleteOutline, EditOutline, PlusOutline, // админ-редактор справочников
  ExpandAltOutline, EyeInvisibleOutline, // «Прибывшие»: разворот в историю, свернуть всё
  LoadingOutline, // спиннер занятости (file-drop, карточки)
  DownloadOutline, // экспорт истории прибывших в Excel
  FileExcelOutline, // «Приём ЛК» в шапке статус-панели главной
  // Сплошные (fill) — как в GTport; сайдбар диспетчера использует именно их.
  HomeFill, EnvironmentFill, EditFill, ClockCircleFill, ToolFill,
  SettingFill, // сайдбар: «Админ»
} from '@ant-design/icons-angular/icons';

import { routes } from './app.routes';
import { CUSTOM_ICONS } from './core/config/custom-icons';
import { authInterceptor } from './core/auth/auth.interceptor';
import { AuthService } from './core/auth/auth.service';

registerLocaleData(localeRu);

// Иконки ng-zorro регистрируем явно (tree-shake). Добавляешь иконку в UI —
// добавь её Outline-определение сюда.
const icons = [
  MenuOutline, UserOutline, LockOutline, LogoutOutline, SettingOutline, AppstoreOutline,
  DashboardOutline, CalendarOutline, ScheduleOutline, DeploymentUnitOutline,
  FileDoneOutline, EnvironmentOutline, DatabaseOutline, ContainerOutline, MailOutline,
  SwapOutline, GlobalOutline, BarChartOutline, LineChartOutline, UploadOutline, InboxOutline,
  ReloadOutline, PrinterOutline, InfoCircleOutline, SyncOutline, CloudDownloadOutline,
  QuestionCircleOutline, DownOutline, RightOutline,
  CheckOutline, BookOutline, CopyOutline, DeleteOutline, EditOutline, PlusOutline,
  ExpandAltOutline, EyeInvisibleOutline, LoadingOutline, DownloadOutline, FileExcelOutline,
  HomeFill, EnvironmentFill, EditFill, ClockCircleFill, ToolFill, SettingFill,
  ...CUSTOM_ICONS,
];

export const appConfig: ApplicationConfig = {
  providers: [
    { provide: LOCALE_ID, useValue: 'ru' },
    provideZoneChangeDetection({ eventCoalescing: true }),
    provideRouter(routes),
    provideHttpClient(withFetch(), withInterceptors([authInterceptor])),
    provideAnimationsAsync(),
    provideNzI18n(ru_RU),
    provideNzIcons(icons),
    // До старта приложения молча восстанавливаем сессию из сохранённого refresh-токена
    // (если был) — чтобы не логиниться заново после перезагрузки страницы.
    provideAppInitializer(() => inject(AuthService).restoreSession()),
  ],
};
