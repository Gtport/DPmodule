<#
.SYNOPSIS
    Накат миграций DPmodule на удалённую базу с машины разработчика (Windows).

.DESCRIPTION
    Гонит golang-migrate из локальной папки migrations на удалённый Postgres.
    Переносить что-либо на сервер не нужно: файлы читаются здесь, выполняются там.

    Скрипт закрывает грабли, на которых легко потерять данные или время:

      • ПАРОЛЬ НЕ В URL. Спецсимволы (%, @, :, /) ломают строку подключения —
        «invalid URL escape». Пароль уходит через PGPASSWORD, кодировать нечего.
      • ПРОВЕРКА СХЕМЫ ДО НАКАТА. 12 из 55 миграций не задают схему явно и
        полагаются на search_path соединения. Придёшь чужой ролью (kgadmin) —
        таблицы уедут в её схему вперемешку с чужими данными. Скрипт
        останавливается, если current_schema() не та.
      • ПРОВЕРКА dirty. Оборванная миграция помечает версию грязной; следующий
        накат откажется идти. Скрипт скажет это внятно и подскажет, как чинить.
      • ПРОВЕРКА ПОСЛЕ. Куда легла schema_migrations, какая версия, сколько
        таблиц. Если таблица учёта оказалась не в своей схеме — это видно сразу,
        а не через полгода.

.EXAMPLE
    .\scripts\db_migrate.ps1 -DbHost 176.53.160.9 -Database kgdm

.EXAMPLE
    # только посмотреть состояние, ничего не накатывая
    .\scripts\db_migrate.ps1 -DbHost 176.53.160.9 -Database kgdm -StatusOnly
#>
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)][string]$DbHost,
    [Parameter(Mandatory = $true)][string]$Database,
    [int]$Port = 5432,
    [string]$Role = 'gtport_app',
    [string]$Schema = 'dpport',
    [string]$SslMode = 'disable',
    [switch]$StatusOnly
)

$ErrorActionPreference = 'Stop'

# ---- инструменты -----------------------------------------------------------
# psql часто стоит, но не прописан в PATH — ищем в типовых местах установки.
function Find-Psql {
    $cmd = Get-Command psql -ErrorAction SilentlyContinue
    if ($cmd) { return $cmd.Source }
    $found = Get-ChildItem 'C:\Program Files\PostgreSQL\*\bin\psql.exe', 'D:\PostgreSQL\*\bin\psql.exe' -ErrorAction SilentlyContinue |
             Sort-Object FullName -Descending | Select-Object -First 1
    if ($found) { return $found.FullName }
    throw "psql не найден. Укажи путь вручную или добавь <PostgreSQL>\bin в PATH."
}

$psql = Find-Psql
if (-not (Get-Command migrate -ErrorAction SilentlyContinue)) {
    throw "migrate не найден в PATH (golang-migrate CLI)."
}

$migrationsDir = Join-Path (Split-Path $PSScriptRoot -Parent) 'migrations'
if (-not (Test-Path $migrationsDir)) { throw "Папка миграций не найдена: $migrationsDir" }

# ---- пароль ----------------------------------------------------------------
# Через окружение, а не в строке подключения: не надо кодировать спецсимволы,
# пароль не светится в выводе ошибок (migrate печатает URL целиком).
if (-not $env:PGPASSWORD) {
    $secure = Read-Host "Пароль роли $Role" -AsSecureString
    $env:PGPASSWORD = [Runtime.InteropServices.Marshal]::PtrToStringAuto(
        [Runtime.InteropServices.Marshal]::SecureStringToBSTR($secure))
}

# search_path в строке — страховка на случай, если настройка роли не применилась.
$dsn = "postgres://$Role@${DbHost}:$Port/$Database" + "?sslmode=$SslMode&search_path=$Schema"

function Invoke-Sql([string]$sql) {
    $out = & $psql $dsn -tAqc $sql 2>&1
    if ($LASTEXITCODE -ne 0) { throw "psql: $out" }
    return ($out | Out-String).Trim()
}

Write-Host "`n=== цель: $Role@${DbHost}:$Port/$Database, схема $Schema" -ForegroundColor Cyan

# ---- 1. подключение и схема ------------------------------------------------
$who = Invoke-Sql "SELECT current_user || '|' || current_schema()"
$parts = $who -split '\|'
Write-Host "подключение: пользователь $($parts[0]), current_schema $($parts[1])"

if ($parts[1] -ne $Schema) {
    throw @"
current_schema = '$($parts[1])', ожидалась '$Schema'. Накат ОСТАНОВЛЕН.
12 из 55 миграций не задают схему явно — они уйдут не туда.
Починить: ALTER ROLE $Role IN DATABASE $Database SET search_path TO $Schema, public;
"@
}

# ---- 2. текущее состояние --------------------------------------------------
$hasTable = Invoke-Sql "SELECT count(*) FROM information_schema.tables WHERE table_schema='$Schema' AND table_name='schema_migrations'"
if ($hasTable -eq '0') {
    Write-Host "schema_migrations нет — это первый накат" -ForegroundColor Yellow
} else {
    $state = Invoke-Sql "SELECT coalesce(version::text,'-') || '|' || coalesce(dirty::text,'-') FROM $Schema.schema_migrations LIMIT 1"
    $s = $state -split '\|'
    Write-Host "текущая версия: $($s[0]), dirty: $($s[1])"
    if ($s[1] -eq 't') {
        throw @"
Версия помечена dirty — предыдущий накат оборвался. Накат ОСТАНОВЛЕН.
Сам DDL откатывается транзакцией, поэтому обычно достаточно очистить учёт:
  1) убедиться, что в схеме нет полуфабрикатов:
     SELECT table_name FROM information_schema.tables WHERE table_schema='$Schema';
  2) если кроме schema_migrations пусто — DELETE FROM $Schema.schema_migrations;
  3) если таблицы всё же создались — снести схему целиком и поднять заново
     (DROP SCHEMA $Schema CASCADE; CREATE SCHEMA $Schema AUTHORIZATION $Role;)
     и заново выполнить scripts/db_bootstrap.sql
"@
    }
}

if ($StatusOnly) {
    Write-Host "`n-v StatusOnly: накат не выполнялся" -ForegroundColor Yellow
    return
}

# ---- 3. накат --------------------------------------------------------------
Write-Host "`n--- migrate up ($migrationsDir)" -ForegroundColor Cyan
$sourceUrl = "file://" + ($migrationsDir -replace '\\', '/')
& migrate -source $sourceUrl -database $dsn up
if ($LASTEXITCODE -ne 0) {
    throw @"
Накат не прошёл. Частая причина — нет права CREATE на самой базе:
«permission denied for database $Database» на миграции 000001, которая
начинается с CREATE SCHEMA IF NOT EXISTS (право проверяется ДО существования).
Лечится под суперпользователем: GRANT CREATE ON DATABASE $Database TO $Role;
"@
}

# ---- 4. проверка результата ------------------------------------------------
Write-Host "`n--- проверка" -ForegroundColor Cyan
$where = Invoke-Sql "SELECT string_agg(table_schema, ',') FROM information_schema.tables WHERE table_name='schema_migrations'"
$state = Invoke-Sql "SELECT version::text || '|' || dirty::text FROM $Schema.schema_migrations LIMIT 1"
$count = Invoke-Sql "SELECT count(*) FROM information_schema.tables WHERE table_schema='$Schema'"
$s = $state -split '\|'

Write-Host "schema_migrations в схеме: $where"
Write-Host "версия: $($s[0]), dirty: $($s[1])"
Write-Host "таблиц в схеме ${Schema}: $count"

if ($where -ne $Schema) {
    Write-Warning "schema_migrations оказалась не в '$Schema' — накат шёл с чужим search_path, состояние надо разбирать вручную."
} elseif ($s[1] -eq 'f') {
    Write-Host "`n=== готово. Дальше — сев справочников (_reference/seed + scripts/seed_directories.sql)" -ForegroundColor Green
}
