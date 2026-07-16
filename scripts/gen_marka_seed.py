#!/usr/bin/env python3
"""Генератор seed-файла marka под групповой ключ (_reference/seed/marka.csv).

Одноразовый инструмент миграции данных (000028): схлопывает полный экспорт
словаря marka старого GTport (ключ по коду груза, sms_1..3, sprav_*, color)
до уникальных сочетаний (okpo, station_kod, cargo_group) с бизнес-атрибуцией.

Перенос полей (решение владельца):
  sms_1 ← старый sms_3 (метка уровня отправитель+станция+группа);
          при конфликте значений внутри ключа берётся КРАТЧАЙШЕЕ (общая часть
          без уточнения груза, напр. «Улак» из «Улак»/«Улак "Г"») + предупреждение;
  sms_3 ← старый sms_2 (регион/направление); при конфликте — ПУСТО + предупреждение
          (для металла старый sms_2 был маркой груза — теперь это расчётный sms_2);
  shipper/client — должны быть однозначны, конфликт = ошибка (падаем громко).

Груз-поля (cargo_kod/cargo_s/cargo_group-как-данные) не переносятся — их даёт
словарь cargo; имя станции погрузки (station) переносится как информационное
(правится вместе с кодом). Результат — CSV под `\\copy marka(okpo,station_kod,
station,cargo_group,shipper,client,sms_1,sms_3)` из scripts/seed_directories.sql.
"""

import argparse
import csv
import sys
from collections import defaultdict


def read_csv(path: str):
    with open(path, encoding="utf-8-sig", newline="") as f:
        for row in csv.DictReader(f):
            yield {k.strip(): (v or "").strip() for k, v in row.items()}


def uniq(rows, field):
    return sorted(set(r[field] for r in rows))


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    ap.add_argument("--src", required=True, help="полный экспорт старого marka (CSV с sms_1..3)")
    ap.add_argument("--out", required=True, help="куда писать seed (обычно _reference/seed/marka.csv)")
    args = ap.parse_args()

    groups = defaultdict(list)
    total = 0
    for r in read_csv(args.src):
        if not r["cargo_group"]:
            print(f"ОШИБКА: пустая cargo_group у строки okpo={r['okpo']} cargo_kod={r['cargo_kod']}", file=sys.stderr)
            return 1
        groups[(int(r["okpo"]), int(r["station_kod"]), r["cargo_group"])].append(r)
        total += 1

    out_rows = []
    for (okpo, station, group), rows in sorted(groups.items()):
        station_name = uniq(rows, "station")[0]
        shippers, clients = uniq(rows, "shipper"), uniq(rows, "client")
        if len(shippers) > 1 or len(clients) > 1:
            print(f"ОШИБКА: конфликт shipper/client у ключа ({okpo},{station},{group}): {shippers} / {clients}", file=sys.stderr)
            return 1

        sms1_variants = uniq(rows, "sms_3")  # новый sms_1 ← старый sms_3
        sms1 = min(sms1_variants, key=len)
        if len(sms1_variants) > 1:
            print(f"конфликт sms_3 у ({okpo},{station},{group}): {sms1_variants} → взято «{sms1}»")

        sms3_variants = uniq(rows, "sms_2")  # новый sms_3 ← старый sms_2
        sms3 = sms3_variants[0]
        if len(sms3_variants) > 1:
            sms3 = ""
            print(f"конфликт sms_2 у ({okpo},{station},{group}): {sms3_variants} → пусто")

        out_rows.append([okpo, station, station_name, group, shippers[0], clients[0], sms1, sms3])

    with open(args.out, "w", encoding="utf-8", newline="") as f:
        w = csv.writer(f)
        w.writerow(["okpo", "station_kod", "station", "cargo_group", "shipper", "client", "sms_1", "sms_3"])
        w.writerows(out_rows)

    print(f"Схлопнуто {total} строк → {len(out_rows)} ключей в {args.out}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
