import { AfterViewInit, Component, ElementRef, EventEmitter, Input, Output, inject } from '@angular/core';

/**
 * Сегментированный ввод индекса поезда в формате 4-3-4 («7438-011-1234»): три поля
 * (4, 3, 4 цифры) с зашитыми дефисами. Печать слева направо — заполнил левое поле,
 * фокус прыгает в правое; Backspace в пустом поле возвращает влево; ←/→ на границе
 * поля переходят между полями. Только цифры. Так исключаются ошибки написания индекса
 * (см. серверную валидацию 4-3-4).
 *
 * Навигация и префилл — прямой работой с DOM (querySelectorAll по хосту), без
 * [value]-биндинга: биндинг value на input мешал программному переводу фокуса.
 *
 * Выход: (valueChange) — собранная строка «dddd-ddd-dddd» либо '' пока не заполнено
 * целиком; (completed) — когда все три поля заполнены (момент «дописал индекс»).
 */
@Component({
  selector: 'app-index-input',
  standalone: true,
  template: `
    <span class="idx">
      <input type="text" inputmode="numeric" maxlength="4" class="s s4" placeholder="7438"
        (input)="onInput($event, 0)" (keydown)="onKeydown($event, 0)" />
      <span class="dash">-</span>
      <input type="text" inputmode="numeric" maxlength="3" class="s s3" placeholder="011"
        (input)="onInput($event, 1)" (keydown)="onKeydown($event, 1)" />
      <span class="dash">-</span>
      <input type="text" inputmode="numeric" maxlength="4" class="s s4" placeholder="1234"
        (input)="onInput($event, 2)" (keydown)="onKeydown($event, 2)" />
    </span>
  `,
  styles: [`
    .idx { display: inline-flex; align-items: center; gap: 3px; }
    .s {
      font-family: var(--font-mono, monospace); font-size: 0.95rem; text-align: center;
      border: 1px solid var(--color-border, #d9d9d9); border-radius: var(--radius-sm, 4px);
      padding: 2px 4px; height: 28px; box-sizing: border-box;
    }
    .s:focus { outline: none; border-color: #1677ff; box-shadow: 0 0 0 2px rgba(22,119,255,0.15); }
    .s4 { width: 52px; }
    .s3 { width: 42px; }
    .dash { color: var(--color-text-secondary, #888); user-select: none; }
  `],
})
export class IndexInputComponent implements AfterViewInit {
  private readonly host = inject(ElementRef<HTMLElement>);
  private readonly lens = [4, 3, 4];
  private initial: string[] = ['', '', ''];

  /** Начальное значение: валидный индекс 4-3-4 раскладывается по полям, иначе — пусто. */
  @Input() set value(v: string | null) {
    const m = /^(\d{4})-(\d{3})-(\d{4})$/.exec((v ?? '').trim());
    this.initial = m ? [m[1], m[2], m[3]] : ['', '', ''];
    this.applyInitial();
  }

  @Output() valueChange = new EventEmitter<string>();
  @Output() completed = new EventEmitter<string>();

  ngAfterViewInit(): void {
    this.applyInitial(); // на этот момент поля уже в DOM
  }

  private inputs(): HTMLInputElement[] {
    return Array.from(this.host.nativeElement.querySelectorAll('input'));
  }

  private applyInitial(): void {
    const ins = this.inputs();
    if (ins.length === 3) ins.forEach((el, i) => (el.value = this.initial[i]));
  }

  onInput(e: Event, i: number): void {
    const el = e.target as HTMLInputElement;
    const digits = el.value.replace(/\D/g, '').slice(0, this.lens[i]);
    if (el.value !== digits) el.value = digits; // откат нецифр/лишнего сразу в DOM
    if (digits.length === this.lens[i] && i < 2) {
      const next = this.inputs()[i + 1];
      next.focus();
      next.select(); // заполнил поле — фокус в следующее (слева направо)
    }
    this.emit();
  }

  onKeydown(e: KeyboardEvent, i: number): void {
    const el = e.target as HTMLInputElement;
    const ins = this.inputs();
    const atStart = (el.selectionStart ?? 0) === 0;
    const atEnd = (el.selectionStart ?? 0) === el.value.length;
    if (e.key === 'Backspace' && el.value === '' && i > 0) {
      e.preventDefault();
      this.focusEnd(ins[i - 1]); // Backspace в пустом поле — возврат влево
    } else if (e.key === 'ArrowLeft' && atStart && i > 0) {
      e.preventDefault();
      this.focusEnd(ins[i - 1]);
    } else if (e.key === 'ArrowRight' && atEnd && i < 2) {
      e.preventDefault();
      ins[i + 1].focus();
      ins[i + 1].setSelectionRange(0, 0);
    }
  }

  private focusEnd(el: HTMLInputElement): void {
    el.focus();
    const n = el.value.length;
    el.setSelectionRange(n, n);
  }

  private emit(): void {
    const v = this.inputs().map((el) => el.value);
    const full = v[0].length === 4 && v[1].length === 3 && v[2].length === 4;
    const val = full ? `${v[0]}-${v[1]}-${v[2]}` : '';
    this.valueChange.emit(val);
    if (full) this.completed.emit(val);
  }
}
