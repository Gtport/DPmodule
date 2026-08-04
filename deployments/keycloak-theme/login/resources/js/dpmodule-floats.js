/* Фон со всплывающими словами и блок «Связаться с поддержкой» для страницы
 * входа. Разметки под них в шаблоне Keycloak нет, а копировать template.ftl
 * ради двух блоков — значит привязать тему к версии Keycloak и ловить поломку
 * на каждом обновлении. Поэтому дорисовываем скриптом: не отработает — форма
 * входа остаётся полностью рабочей, просто без украшений.
 *
 * Слова, координаты (% от экрана), кегль и тайминги перенесены один в один из
 * прежней своей формы (pages/login/login.component.ts до коммита 12ce9f4).
 */
(function () {
  'use strict';

  var FLOATS = [
    { text: 'Мониторинг',             x: 16, y: 72, size: 52, delay: 0.0, dur: 9.5 },
    { text: 'Аналитика',              x: 84, y: 66, size: 44, delay: 1.6, dur: 10.5 },
    { text: 'Прогнозирование',        x: 24, y: 26, size: 36, delay: 3.2, dur: 11.5 },
    { text: 'Моделирование',          x: 80, y: 24, size: 42, delay: 4.8, dur: 10.0 },
    { text: 'Флот',                   x: 33, y: 12, size: 52, delay: 6.4, dur: 9.0 },
    { text: 'Карта движения поездов', x: 30, y: 88, size: 29, delay: 8.0, dur: 12.0 },
    { text: 'Претензионная работа',   x: 72, y: 90, size: 29, delay: 9.6, dur: 11.2 },
    { text: 'Дислокация',             x: 88, y: 44, size: 44, delay: 2.4, dur: 12.5 },
    { text: 'Погрузка',               x: 10, y: 48, size: 48, delay: 5.6, dur: 11.0 },
    { text: 'Склад',                  x: 62, y: 14, size: 52, delay: 8.8, dur: 9.8 }
  ];

  var SUPPORT_EMAIL = 'help@gtport.com';

  function renderFloats() {
    var box = document.createElement('div');
    box.className = 'dp-floats';
    box.setAttribute('aria-hidden', 'true');

    FLOATS.forEach(function (w) {
      var el = document.createElement('span');
      el.className = 'dp-float';
      el.textContent = w.text;
      el.style.left = w.x + '%';
      el.style.top = w.y + '%';
      el.style.fontSize = w.size + 'px';
      el.style.animationDelay = w.delay + 's';
      el.style.animationDuration = w.dur + 's';
      box.appendChild(el);
    });

    document.body.insertBefore(box, document.body.firstChild);
  }

  function renderContact() {
    var card = document.querySelector('.card-pf');
    if (!card) return;

    var wrap = document.createElement('div');
    wrap.className = 'dp-contact';

    var link = document.createElement('button');
    link.type = 'button';
    link.className = 'dp-contact__link';
    link.textContent = '✉ Связаться с поддержкой';

    link.addEventListener('click', function () {
      var box = document.createElement('div');
      box.className = 'dp-contact__email';

      var addr = document.createElement('span');
      addr.className = 'dp-contact__addr';
      addr.textContent = SUPPORT_EMAIL;

      var copy = document.createElement('button');
      copy.type = 'button';
      copy.className = 'dp-contact__copy';
      copy.textContent = 'Копировать';

      copy.addEventListener('click', function () {
        // clipboard доступен только в защищённом контексте (у нас HTTPS);
        // если его нет — адрес всё равно виден и его можно выделить руками.
        if (navigator.clipboard) {
          navigator.clipboard.writeText(SUPPORT_EMAIL);
          copy.textContent = '✓ Скопировано';
          setTimeout(function () { copy.textContent = 'Копировать'; }, 2000);
        }
      });

      box.appendChild(addr);
      box.appendChild(copy);
      wrap.replaceChild(box, link);
    });

    wrap.appendChild(link);
    card.appendChild(wrap);
  }

  function init() {
    try {
      renderFloats();
      renderContact();
    } catch (e) {
      // Украшения не должны мешать входу ни при каких обстоятельствах.
    }
  }

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', init);
  } else {
    init();
  }
})();
