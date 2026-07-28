# Plan: Автообновление (атомарный вариант, GitHub Releases)

## Цель

Добавить в Vecura проверку и применение обновлений напрямую из GitHub Releases.
Приложение при запуске и по кнопке в Settings сверяет свою версию с последним
релизом, скачивает новый бинарь и **атомарно** заменяет себя с авто-рестартом.
Без инсталлятора, без прав админа (если папка с exe доступна на запись).

## Источник обновлений

GitHub Releases публичного репозитория:

```
GET https://api.github.com/repos/<owner>/<repo>/releases/latest
```

Ответ (упрощённо):
```json
{
  "tag_name": "v1.2.0",
  "name": "Vecura 1.2.0",
  "body": "Changelog...",
  "assets": [
    { "name": "vecura-1.2.0.exe", "browser_download_url": "https://.../vecura-1.2.0.exe", "size": 12345678 }
  ]
}
```

- `<owner>/<repo>` берётся из конфига / env `VECURA_UPDATE_REPO` (по умолчанию `yulta/Vecura`).
- Версия релиза = `tag_name` без префикса `v` (→ `1.2.0`).
- Asset выбирается по шаблону `vecura-<version>.exe` (case-insensitive).
- Для приватного репозитория: `Authorization: Bearer <token>` из env `GITHUB_TOKEN`
  (токен НЕ шьётся в бинарь, только через env у пользователя).

## Архитектура обновления

```
Vecura (основной процесс)
  │
  ├─ CheckForUpdates()  ──GET──▶  GitHub API /releases/latest
  │       │                         (semver compare)
  │       ▼
  │   [есть новее?] ──нет──▶ return "up to date"
  │       │ да
  │       ▼
  ├─ DownloadUpdate()  ──GET──▶  asset.browser_download_url
  │       │                      (в temp, с прогрессом)
  │       ▼
  ├─ ApplyUpdate()  ──spawn──▶  updater.exe <args>  ──▶  os.Exit(0)
  │                                  │
  │                                  ├─ ждёт завершения Vecura
  │                                  ├─ переименовывает vecura.exe → vecura.old.exe
  │                                  ├─ копирует скачанный файл → vecura.exe
  │                                  └─ запускает vecura.exe (новый)
  └─ (процесс завершён)
```

**Почему отдельный `updater.exe`:** Windows не позволяет переименовать/перезаписать
запущенный `.exe`. Внешний процесс, запущенный с аргументами, дожидается выхода
основного, меняет файлы и рестартит. Это классический и надёжный паттерн.

## Структура файлов

```
Vecura/
├── main.go                      # Version string + передача в app
├── wails.json                   # productversion → читается в Version
├── cmd/
│   └── updater/
│       └── main.go              # атомарная замена + рестарт
├── internal/
│   └── update/
│       ├── update.go            # Check / Download / Apply
│       ├── github.go            # запрос к GitHub API
│       └── version.go           # семвер-сравнение, парсинг tag
└── frontend/
    └── src/
        ├── views/SettingsView.vue   # кнопка + прогресс + статус
        └── App.vue                   # авто-проверка при старте
```

---

## Детали реализации

### 1. Версия приложения

**`main.go`** — добавить переменную, читаемую из `wails.json` при старте:

```go
// Version заполняется из wails.json (info.productversion) при старте.
var Version = "dev"

func main() {
    // читаем wails.json, достаём info.productversion → Version
    // (или через go:embed wails.json)
    app := api.NewApp(...)
    app.SetVersion(Version)
    // ...
}
```

**`wails.json`** — уже есть `info.productversion: "1.0.0"`. Используем как источник
истины. При релизе меняем только его (и тег в git).

**`internal/api/app.go`** — добавить:

```go
type App struct {
    // ...существующие поля...
    version string
}
func (a *App) SetVersion(v string) { a.version = v }
func (a *App) Version() string    { return a.version }
```

### 2. `internal/update/version.go` — семвер

Зависимость: `github.com/Masterminds/semver/v3` (легковесная, только парсинг/сравнение).

```go
func IsNewer(remote, current string) (bool, error) {
    r, err := semver.NewVersion(strings.TrimPrefix(remote, "v"))
    if err != nil { return false, err }
    c, err := semver.NewVersion(strings.TrimPrefix(current, "v"))
    if err != nil { return false, err }
    return r.GreaterThan(c), nil
}
```

### 3. `internal/update/github.go` — запрос релиза

```go
type Release struct {
    TagName string `json:"tag_name"`
    Name    string `json:"name"`
    Body    string `json:"body"`
    Assets  []struct {
        Name               string `json:"name"`
        BrowserDownloadURL string `json:"browser_download_url"`
        Size               int64  `json:"size"`
    } `json:"assets"`
}

func LatestRelease(repo string, token string) (*Release, error) {
    url := "https://api.github.com/repos/" + repo + "/releases/latest"
    req, _ := http.NewRequest("GET", url, nil)
    req.Header.Set("Accept", "application/vnd.github+json")
    if token != "" { req.Header.Set("Authorization", "Bearer "+token) }
    // User-Agent обязателен для GitHub API
    req.Header.Set("User-Agent", "Vecura-Updater")
    // ... GET, декод JSON, выбрать asset "vecura-<tag>.exe"
}
```

**Нюансы GitHub API:**
- `User-Agent` обязателен (без него 403).
- Rate limit для анонимов: 60 req/hour — для ручной проверки хватает.
- При ошибке сети / 403 / rate limit — возвращаем ошибку, UI показывает
  «не удалось проверить» (не блокирует работу приложения).

### 4. `internal/update/update.go` — оркестрация

```go
type Progress struct {
    Downloaded int64
    Total      int64
    Phase      string // "checking" | "downloading" | "applying" | "done" | "error"
    Error      string
}

func Check(repo, token, currentVersion string) (*Release, bool, error)
func Download(rel *Release, dest string, onProgress func(Progress)) error
func Apply(exePath, downloadedPath string) error {
    // spawn updater.exe с аргументами, затем os.Exit(0)
    cmd := exec.Command(updaterPath,
        "--target", exePath,        // vecura.exe (себя)
        "--source", downloadedPath, // скачанный файл
        "--restart", exePath,       // что запустить после
    )
    cmd.Start()
    os.Exit(0)
}
```

### 5. `cmd/updater/main.go` — атомарная замена

```go
func main() {
    // парсим флаги --target --source --restart
    // 1. ждём, пока target-процесс (Vecura) завершится (он сам делает os.Exit)
    //    или убиваем по PID, переданному через --pid
    // 2. переименовываем target → target + ".old" (если есть — удаляем старый .old)
    // 3. копируем source → target
    // 4. запускаем target (--restart) с теми же аргументами
    // 5. выходим
}
```

**Атомарность:**
- Запись во временный файл (`%TEMP%/vecura-download.exe`), НЕ поверх рабочего.
- `rename` старого в `.old`, затем `rename` нового на место — на NTFS атомарно.
- Если новый не запустился — роллбэк из `.old`.
- `.old` оставляем для возможности отката (удаляем при следующем успешном обновлении).

### 6. API-биндинги (`internal/api/app.go`)

```go
func (a *App) CheckForUpdates() (map[string]interface{}, error) {
    rel, newer, err := update.Check(repo, token, a.version)
    return map[string]interface{}{
        "current": a.version,
        "latest":  rel.TagName,
        "newer":   newer,
        "notes":   rel.Body,
        "error":   errMsg(err),
    }, nil
}

func (a *App) DownloadAndApplyUpdate() error {
    rel, newer, err := update.Check(...)
    if !newer { return nil }
    tmp := filepath.Join(os.TempDir(), "vecura-update.exe")
    update.Download(rel, tmp, onProgress)   // прогресс через runtime.Events
    self, _ := os.Executable()
    update.Apply(self, tmp)                 // spawn updater + os.Exit
}
```

Прогресс скачивания → `runtime.Events.Emit("update-progress", ...)` для живого UI.

### 7. Frontend — SettingsView.vue

Добавить секцию "Updates":
- Кнопка **Check for updates** → `CheckForUpdates()`
- Если `newer`: показать версию + чейнджлог (`notes`) + кнопку **Update now**
- Прогресс-бар во время `DownloadAndApplyUpdate()` (слушаем `update-progress`)
- Статус: "Up to date" / "Update available" / "Error: ..."
- Текущая версия: `Version()` при mount

### 8. Frontend — App.vue

При `onStartup` (или mount) — фоновый вызов `CheckForUpdates()`,
и если `newer` — тост «Доступно обновление до X.X.X».

---

## Сборка

### `wails.json` — добавить секцию build

```json
{
  "info": { "productversion": "1.0.0" },
  "build:frontend": "npm run build",
  "build:updater": "go build -o build/bin/updater.exe ./cmd/updater"
}
```

### Скрипт сборки (PowerShell или bash)

```bash
wails build              # собирает vecura.exe в build/bin/
go build -o build/bin/updater.exe ./cmd/updater   # собираем updater
```

`updater.exe` должен лежать рядом с `vecura.exe` (в `build/bin/`), чтобы
основной процесс мог его найти через `filepath.Join(filepath.Dir(self), "updater.exe")`.

### Релиз (GitHub)

1. Меняем `info.productversion` в `wails.json` → `1.2.0`
2. `git tag v1.2.0 && git push --tags`
3. `wails build` + `go build ./cmd/updater`
4. Создаём GitHub Release `v1.2.0`, прикладываем asset `vecura-1.2.0.exe`
   (имя = `vecura-<tag>.exe`, без префикса `v`)
5. Готово — приложение увидит релиз при следующей проверке

---

## Зависимости

| Пакет | Назначение | Размер |
|-------|-----------|--------|
| `github.com/Masterminds/semver/v3` | семвер-парсинг/сравнение | ~30KB |

Без CGO, без тяжёлых зависимостей. `golang.org/x/sys` уже есть.

---

## Риски и ограничения

| Риск | Митигация |
|------|-----------|
| Windows блокирует запущенный exe | Внешний `updater.exe` делает замену после выхода Vecura |
| Антивирус ругается на скачивание/замену exe | Подпись кода (позже); для личного использования — исключение в AV |
| Rate limit GitHub API (60/h) | Кэшируем результат на сессию; проверка только по кнопке + раз в N минут при старте |
| Нет прав на запись в папку с exe (Program Files) | Для per-user установки (AppData/Local) прав хватает; в Program Files нужен админ — тогда показываем «скачайте установщик» |
| Битый скачанный файл | Проверка размера против `asset.size` + опц. SHA256 из релиза |
| Роллбэк при неудаче | Сохраняем `.old.exe`, при ошибке запуска — возврат |

---

## Статус реализации (план)

| Этап | Статус | Файл |
|------|--------|------|
| Version из wails.json | ПЛАН | `main.go`, `wails.json` |
| semver compare | ПЛАН | `internal/update/version.go` |
| GitHub API client | ПЛАН | `internal/update/github.go` |
| Check/Download/Apply | ПЛАН | `internal/update/update.go` |
| updater.exe (atomic replace) | ПЛАН | `cmd/updater/main.go` |
| API bindings | ПЛАН | `internal/api/app.go` |
| SettingsView UI | ПЛАН | `frontend/src/views/SettingsView.vue` |
| Auto-check on startup | ПЛАН | `frontend/src/App.vue` |
| Build script (updater) | ПЛАН | `wails.json` + скрипт |
| Release workflow | ПЛАН | документация в плане |

---

## Чеклист готовности

- [ ] `Version` читается из `wails.json` и отдаётся через `Version()`
- [ ] `CheckForUpdates` возвращает корректный `newer` для тестового релиза
- [ ] `DownloadAndApplyUpdate` качает и спавнит `updater.exe`
- [ ] `updater.exe` атомарно заменяет exe и рестартит
- [ ] UI показывает прогресс и чейнджлог
- [ ] Авто-проверка при старте не блокирует запуск при офлайне
- [ ] Роллбэк из `.old.exe` работает

---

## Рекомендации

1. **Тестируй локально** через фейковый релиз (положи `update.json` на локальный
   сервер и временно переключи `repo` на него) — не спами GitHub API.
2. **Не шей токен** в бинарь. Для приватного репозитория — только через env
   `GITHUB_TOKEN` у пользователя.
3. **Подпиши exe** (через `signtool`) перед публичным релизом — Windows SmartScreen
   иначе будет пугать юзеров.
4. **Имя asset строго** `vecura-<version>.exe` (без `v`), иначе парсер не найдёт.
