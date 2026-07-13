import { Component, ElementRef, EventEmitter, Input, Output, QueryList, ViewChildren, signal } from '@angular/core';

/**
 * Сегментированный ввод индекса поезда в формате 4-3-4 («7438-011-1234»): три поля
 * (4, 3, 4 цифры) с зашитыми дефисами. Печать слева направо — заполнил левое поле,
 * фокус прыгает в правое; Backspace в пустом поле возвращает влево. Только цифры.
 * Так исключаются ошибки написания индекса (см. серверную валидацию 4-3-4).
 *
 * Выход: (valueChange) — собранная строка «dddd-ddd-dddd», либо '' пока индекс не
 * заполнен целиком; (completed) — когда все три поля заполнены (момент «дописал»).
 */
@Component({
  selector: 'app-index-input',
  standalone: true,
  template: `
    <span class="idx">
      <input #seg type="text" inputmode="numeric" maxlength="4" class="s s4" placeholder="7438"
        [value]="p1()" (input)="onInput(0, $event)" (keydown)="onKeydown(0, $event)" />
      <span class="dash">-</span>
      <input #seg type="text" inputmode="numeric" maxlength="3" class="s s3" placeholder="011"
        [value]="p2()" (input)="onInput(1, $event)" (keydown)="onKeydown(1, $event)" />
      <span class="dash">-</span>
      <input #seg type="text" inputmode="numeric" maxlength="4" class="s s4" placeholder="1234"
        [value]="p3()" (input)="onInput(2, $event)" (keydown)="onKeydown(2, $event)" />
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
export class IndexInputComponent {
  /** Начальное значение: если это валидный индекс 4-3-4 — раскладывается по полям. */
  @Input() set value(v: string | null) {
    const m = /^(\d{4})-(\d{3})-(\d{4})$/.exec((v ?? '').trim());
    if (m) {
      this.p1.set(m[1]);
      this.p2.set(m[2]);
      this.p3.set(m[3]);
    }
  }

  @Output() valueChange = new EventEmitter<string>();
  @Output() completed = new EventEmitter<string>();

  @ViewChildren('seg') private segs!: QueryList<ElementRef<HTMLInputElement>>;

  readonly p1 = signal('');
  readonly p2 = signal('');
  readonly p3 = signal('');

  private readonly lens = [4, 3, 4];
  private readonly sigs = [this.p1, this.p2, this.p3];

  onInput(i: number, e: Event): void {
    const el = e.target as HTMLInputElement;
    const digits = el.value.replace(/\D/g, '').slice(0, this.lens[i]);
    el.value = digits; // жёстко откатываем нецифры/лишнее сразу в DOM (CD может не тронуть при равном сигнале)
    this.sigs[i].set(digits);
    if (digits.length === this.lens[i] && i < 2) {
      this.focusSeg(i + 1); // заполнил поле — фокус в следующее (слева направо)
    }
    this.emit();
  }

  onKeydown(i: number, e: KeyboardEvent): void {
    const el = e.target as HTMLInputElement;
    if (e.key === 'Backspace' && el.value === '' && i > 0) {
      e.preventDefault();
      this.focusSeg(i - 1); // Backspace в пустом поле — возврат влево
    }
  }

  private focusSeg(i: number): void {
    const el = this.segs?.get(i)?.nativeElement;
    if (el) {
      el.focus();
      el.select();
    }
  }

  private emit(): void {
    const full = this.p1().length === 4 && this.p2().length === 3 && this.p3().length === 4;
    const val = full ? `${this.p1()}-${this.p2()}-${this.p3()}` : '';
    this.valueChange.emit(val);
    if (full) this.completed.emit(val);
  }
}
