#!/usr/bin/env python3
"""Генератор seed-файла словаря грузов (_reference/seed/cargo.csv).

Одноразовый инструмент подготовки данных: собирает полный справочник грузов
ЕТСНГ и накладывает поверх ручную переработку. Результат — CSV под
`\\copy cargo(cargo_kod,name,cargo_group,cargo_s,cargo_sms)` из
scripts/seed_directories.sql.

Источники:
  --full    полный перечень грузов (cargo_kod;cargo_group;name;name_s,
            разделитель `;`, официальные группы ЕТСНГ);
  --rework  переработанная часть (cargo_kod;cargo_group;name;cargo_s) —
            бизнес-группы (УГОЛЬ/МЕТАЛЛ/ЧУГУН) и краткие имена; её значения
            имеют приоритет над полным перечнем.

cargo_sms генерируется правилом (см. cargo_sms()) для ВСЕХ строк; коллизии
внутри бизнес-групп печатаются в отчёт — их правят руками в готовом CSV.
Повторный запуск ЗАТИРАЕТ ручные правки: после правки файла источник правды —
сам _reference/seed/cargo.csv, генератор больше не запускать.
"""

import argparse
import csv
import sys
from collections import defaultdict

# Не-окончания для сокращения: гласные + мягкий/твёрдый знак.
NON_FINAL = set("АЕЁИОУЫЭЮЯЬЪ")


def cargo_sms(cargo_s: str) -> str:
    """Метка груза по правилу владельца.

    1. Два слова и первое «УГОЛЬ» — берём то, что после «УГОЛЬ».
    2. Длиннее 5 символов — сокращаем до 4 (или 3) символов так, чтобы
       заканчивалось на согласную; иначе — первые 4 как есть.
    """
    s = cargo_s.strip()
    words = s.split()
    if len(words) == 2 and words[0] == "УГОЛЬ":
        s = words[1]
    if len(s) <= 5:
        return s
    for n in (4, 3):
        ch = s[n - 1]
        if ch not in NON_FINAL and not ch.isspace():
            return s[:n]
    return s[:4]


def read_csv(path: str, delimiter: str = ";"):
    """Читает CSV, снимая BOM и CRLF (файлы приходят из Windows-выгрузок)."""
    with open(path, encoding="utf-8-sig", newline="") as f:
        for row in csv.DictReader(f, delimiter=delimiter):
            yield {k.strip(): (v or "").strip() for k, v in row.items()}


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    ap.add_argument("--full", required=True, help="полный перечень грузов (cargo_full.csv)")
    ap.add_argument("--rework", required=True, help="переработанная часть (cargo.csv)")
    ap.add_argument("--out", required=True, help="куда писать seed (обычно _reference/seed/cargo.csv)")
    args = ap.parse_args()

    # 1. Полный перечень: базовые строки (группа официальная, cargo_s = name).
    rows = {}  # cargo_kod -> dict
    for r in read_csv(args.full):
        kod = r["cargo_kod"]
        if kod in rows:
            print(f"ВНИМАНИЕ: дубль cargo_kod={kod} в {args.full} — оставлена первая строка", file=sys.stderr)
            continue
        rows[kod] = {
            "cargo_kod": kod,
            "name": r["name"],
            "cargo_group": r["cargo_group"],
            "cargo_s": r["name_s"] or r["name"],
            "reworked": False,
        }

    # 2. Оверлей переработки: бизнес-группа и краткое имя имеют приоритет.
    missing = []
    for r in read_csv(args.rework):
        kod = r["cargo_kod"]
        base = rows.get(kod)
        if base is None:
            missing.append(kod)
            continue
        base["cargo_group"] = r["cargo_group"]
        base["cargo_s"] = r["cargo_s"]
        base["reworked"] = True
    if missing:
        print(f"ВНИМАНИЕ: {len(missing)} кодов переработки нет в полном перечне: {missing}", file=sys.stderr)

    # 3. cargo_sms по правилу — всем строкам.
    for row in rows.values():
        row["cargo_sms"] = cargo_sms(row["cargo_s"])

    # 4. Отчёт о коллизиях cargo_sms внутри переработанных групп (правятся руками).
    by_group_sms = defaultdict(set)
    for row in rows.values():
        if row["reworked"]:
            by_group_sms[(row["cargo_group"], row["cargo_sms"])].add(row["cargo_s"])
    collisions = {k: v for k, v in sorted(by_group_sms.items()) if len(v) > 1}
    if collisions:
        print("Коллизии cargo_sms внутри групп (поправить руками в готовом CSV):")
        for (group, sms), names in collisions.items():
            print(f"  {group} / {sms}: {sorted(names)}")

    # 5. Запись seed: колонки в порядке \copy из seed_directories.sql.
    out_rows = sorted(rows.values(), key=lambda r: int(r["cargo_kod"]))
    with open(args.out, "w", encoding="utf-8", newline="") as f:
        w = csv.writer(f)
        w.writerow(["cargo_kod", "name", "cargo_group", "cargo_s", "cargo_sms"])
        for row in out_rows:
            w.writerow([row["cargo_kod"], row["name"], row["cargo_group"], row["cargo_s"], row["cargo_sms"]])

    reworked = sum(1 for r in rows.values() if r["reworked"])
    print(f"Записано {len(out_rows)} грузов в {args.out} (переработанных: {reworked}, коллизий: {len(collisions)})")
    return 0


if __name__ == "__main__":
    sys.exit(main())
