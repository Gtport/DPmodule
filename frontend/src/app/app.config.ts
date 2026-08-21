import {
  ApplicationConfig,
  LOCALE_ID,
  provideZoneChangeDetection,
} from '@angular/core';
import { provideRouter } from '@angular/router';
import { provideHttpClient, withInterceptors, withFetch } from '@angular/common/http';
import { provideAnimationsAsync } from '@angular/platform-browser/animations/async';
import { registerLocaleData } from '@angular/common';
import localeRu from '@angular/common/locales/ru';

import { provideNzI18n, ru_RU } from 'ng-zorro-antd/i18n';
import { provideNzIcons } from 'ng-zorro-antd/icon';
import {
  MenuOutline, UserOutline, LogoutOutline, SettingOutline, AppstoreOutline,
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
  SendOutline, PictureOutline, MessageOutline, // экран «Рассылка»: MAX-картинкой/текстом
  SearchOutline, FileSearchOutline, FilterOutline, HistoryOutline, // отчёт «Брошенные»: поиск/фильтр/журнал
  CameraOutline, // «Погрузка»/«Выгрузка за день»: сохранить PNG
  ClearOutline, // «Сброс» («Поезда в движении», «История вагонов»)
  UpOutline, WarningOutline, CheckCircleOutline, // «История вагонов»: свёртка фильтров, просрочка доставки
  CaretUpOutline, CaretDownOutline, // «История вагонов»: направление сортировки колонки
  BgColorsOutline, // «Прогноз прибытия/выгрузки»: скрыть окраску клиентов
  BellOutline, CloseCircleOutline, ToolOutline, // колокольчик уведомлений: сам колокольчик + типы error/service
  FileExcelOutline, // «Просрочка доставки»: отчёт для претензии
  // Сплошные (fill) — как в GTport; сайдбар диспетчера использует именно их.
  HomeFill, EnvironmentFill, EditFill, ClockCircleFill, ToolFill,
  SettingFill, // сайдбар: «Админ»
} from '@ant-design/icons-angular/icons';

import {
  provideKeycloak,
  createInterceptorCondition,
  INCLUDE_BEARER_TOKEN_INTERCEPTOR_CONFIG,
  includeBearerTokenInterceptor,
  type IncludeBearerTokenCondition,
} from 'keycloak-angular';

import { routes } from './app.routes';
import { environment } from '../environments/environment';
import { CUSTOM_ICONS } from './core/config/custom-icons';

registerLocaleData(localeRu);

// Иконки ng-zorro регистрируем явно (tree-shake). Добавляешь иконку в UI —
// добавь её Outline-определение сюда.
const icons = [
  MenuOutline, UserOutline, LogoutOutline, SettingOutline, AppstoreOutline,
  DashboardOutline, CalendarOutline, ScheduleOutline, DeploymentUnitOutline,
  FileDoneOutline, EnvironmentOutline, DatabaseOutline, ContainerOutline, MailOutline,
  SwapOutline, GlobalOutline, BarChartOutline, LineChartOutline, UploadOutline, InboxOutline,
  ReloadOutline, PrinterOutline, InfoCircleOutline, SyncOutline, CloudDownloadOutline,
  QuestionCircleOutline, DownOutline, RightOutline,
  CheckOutline, BookOutline, CopyOutline, DeleteOutline, EditOutline, PlusOutline,
  ExpandAltOutline, EyeInvisibleOutline, LoadingOutline, DownloadOutline,
  SendOutline, PictureOutline, MessageOutline,
  SearchOutline, FileSearchOutline, FilterOutline, HistoryOutline, CameraOutline,
  ClearOutline, UpOutline, WarningOutline, CheckCircleOutline, CaretUpOutline, CaretDownOutline,
  BgColorsOutline, BellOutline, CloseCircleOutline, ToolOutline, FileExcelOutline,
  HomeFill, EnvironmentFill, EditFill, ClockCircleFill, ToolFill, SettingFill,
  ...CUSTOM_ICONS,
];

// Bearer вешаем ТОЛЬКО на бэкенд модуля — чтобы токен не утекал в чужие
// сервисы. Интерсептор keycloak-angular сам тихо обновляет токен перед запросом.
// Границу строим из ТОГО ЖЕ apiBaseUrl, которым все сервисы собирают URL
// (под path-адресацией это '/dpport/api', на прежних стендах '/api'), —
// жёсткая регулярка «^/api» под префиксом перестала бы матчиться, и все
// запросы ушли бы без токена. Опциональный origin — на случай абсолютного
// 'http://host:8080/api/...'.
const apiPathEscaped = environment.apiBaseUrl.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
const apiBearerCondition = createInterceptorCondition<IncludeBearerTokenCondition>({
  urlPattern: new RegExp(`^(https?://[^/]+)?${apiPathEscaped}(/|$)`, 'i'),
  bearerPrefix: 'Bearer',
});

export const appConfig: ApplicationConfig = {
  providers: [
    { provide: LOCALE_ID, useValue: 'ru' },
    provideZoneChangeDetection({ eventCoalescing: true }),
    provideRouter(routes),
    // Auth Code + PKCE (S256), hosted-вход Keycloak. login-required → неавторизованных
    // сразу уводит на страницу входа Keycloak (весь модуль за авторизацией).
    // Пустой keycloak.url (окружение разработчика) даёт относительный /realms/... —
    // Keycloak выведен тем же прокси, что и приложение, запросы same-origin.
    provideKeycloak({
      config: {
        url: environment.keycloak.url,
        realm: environment.keycloak.realm,
        clientId: environment.keycloak.clientId,
      },
      initOptions: {
        onLoad: 'login-required',
        pkceMethod: 'S256',
        checkLoginIframe: false,
      },
      providers: [
        { provide: INCLUDE_BEARER_TOKEN_INTERCEPTOR_CONFIG, useValue: [apiBearerCondition] },
      ],
    }),
    provideHttpClient(withFetch(), withInterceptors([includeBearerTokenInterceptor])),
    provideAnimationsAsync(),
    provideNzI18n(ru_RU),
    provideNzIcons(icons),
  ],
};
