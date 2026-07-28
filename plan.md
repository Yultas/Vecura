# Plan: GPU Detection + llama.cpp Runtime + VL Embedding Model

## Цель

Добавить в Vecura поддержку vision-language моделей через llama.cpp для поиска изображений по визуальному содержимому (а не только по текстовому промпту).

## Архитектура

```
~/.vecura/
├── gallery.db          # SQLite (уже есть)
├── config.json         # конфиг (расширяем)
├── thumbnails/         # превью (уже есть)
└── llama/              # НОВОЕ - llama.cpp runtime
    ├── bin/
    │   ├── cuda-12.4/  # или vulkan/ или cpu/
    │   │   ├── llama-server.exe
    │   │   └── *.dll
    ├── runtime.json    # какой runtime установлен
    ├── server.pid      # PID запущенного сервера
    ├── server.log      # лог сервера
    └── models/         # .gguf модели
        ├── mmproj.gguf
        └── model.gguf
```

---

## Статус реализации

### СДЕЛАНО

| Этап | Статус | Файл |
|------|--------|------|
| GPU Detection | ГОТОВО | `internal/gpu/detect.go` |
| Runtime Manager | ГОТОВО | `internal/llama/runtime.go` |
| Zip extraction | ГОТОВО | `internal/llama/extract.go` |
| Server process mgmt | ГОТОВО | `internal/llama/server.go` |
| VL Embedder | ГОТОВО | `internal/embedder/llama_vl.go` |
| API methods | ГОТОВО | `internal/api/app.go` |
| Registry local | ГОТОВО | `internal/models/registry.go` |
| main.go wiring | ГОТОВО | `main.go` |
| Wails bindings | ГОТОВО | `frontend/wailsjs/go/api/App.js` |
| SettingsView UI | ГОТОВО | `frontend/src/views/SettingsView.vue` |
| SearchBar image btn | ГОТОВО | `frontend/src/components/SearchBar.vue` |
| App.vue integration | ГОТОВО | `frontend/src/App.vue` |

### НЕ СДЕЛАНО (отложено)

| Этап | Описание | Причина |
|------|----------|---------|
| Scan Pipeline vision path | Автоматическое кодирование VL при скании | Требует работающий сервер + модель |
| DB Migration (embedding_type) | Колонка embedding_type в embeddings | MVP работает без неё — VL хранится как provider=local |
| Scan pipeline vision path | Параллельный VL embedding при сканировании | Deferred until testing with real models |

---

## Детали реализации (что сделано)

### Часть 1: GPU Detection — ГОТОВО

#### `internal/gpu/detect.go`

- Структура `GPUInfo` (Vendor, Name, VRAM)
- `DetectGPU()` — nvidia-smi + PowerShell WMI fallback
- `vendorFromName()` — эвристическое определение вендора

### Часть 2: llama.cpp Runtime Manager — ГОТОВО

#### `internal/llama/runtime.go`

- `RuntimeManager` с baseDir `~/.vecura/llama`
- `Status() RuntimeInfo` — проверяет runtime.json + наличие llama-server.exe
- `Download(backend, progress)` — скачивает с GitHub releases (b5499+)
- `ServerPath()` — полный путь к llama-server.exe
- `BinDir()` — директория с бинарниками (для поиска DLL)
- `ModelsDir()` — путь к models/
- Метаданные в runtime.json: `{version, backend}`

Asset naming (актуально для b10154):
```
cuda-12.4: llama-b10154-bin-win-cuda-12.4-x64.zip
cuda-13.3: llama-b10154-bin-win-cuda-13.3-x64.zip
vulkan:    llama-b10154-bin-win-vulkan-x64.zip
cpu:       llama-b10154-bin-win-cpu-x64.zip
```

#### `internal/llama/extract.go`

- `extractZip(src, destDir)` — распаковка zip с strip верхней директории
- Защита от zip-slip

#### `internal/llama/server.go`

- `Server` с cmd, pid, port, baseDir
- `Start(modelPath, mmprojPath)` — запуск в фоне с `CREATE_NEW_PROCESS_GROUP`
- `Stop()` — graceful shutdown через os.Interrupt + таймаут 5 сек + Kill
- `IsRunning()` — проверка PID через syscall.Signal(0)
- `WaitForReady(timeout)` — polling `/health` endpoint
- PID хранение: `~/.vecura/llama/server.pid`
- Лог: `~/.vecura/llama/server.log`

### Часть 3: VL Embedder — ГОТОВО

#### `internal/embedder/llama_vl.go`

- `LLamaVLEmbedder` — HTTP клиент к llama-server
- `Embed(texts)` — текстовые эмбеддинги через `/v1/embeddings`
- `EmbedImage(imageData)` — кодирует изображение как base64 data URI
- `EmbedImages(images)` — batch обработка
- `EmbedImageFile(path)` — чтение файла + EmbedImage
- `detectMIME()` — определение типа по сигнатуре (PNG/JPEG/GIF/WebP)
- Ключ: `local/vl`

### Часть 4: API methods — ГОТОВО

#### `internal/api/app.go`

Добавлено в `appConfig`:
```go
LLamaBackend string `json:"llamaBackend"`
VLModelPath  string `json:"vlModelPath"`
VLProjPath   string `json:"vlProjPath"`
VLModelDim   int    `json:"vlModelDim"`
```

Добавлено в `App` struct:
```go
llamaRM      *llama.RuntimeManager
llamaServer  *llama.Server
vlEmbedder   *embedder.LLamaVLEmbedder
vlModelPath  string
vlProjPath   string
vlModelDim   int
```

Новые методы:
- `DetectGPU() (*GPUInfoResult, error)`
- `GetLLamaStatus() (*LLamaStatus, error)`
- `DownloadLLama(backend string) error`
- `SelectGGUFFile(kind string) (string, error)`
- `SetVLModel(modelPath, projPath string, dim int) error`
- `SearchByImage(imagePath string, K int) ([]SearchHit, error)`
- `SearchByImageDataURI(dataURI string, K int) ([]SearchHit, error)`
- `Shutdown(ctx context.Context)` — останавливает llama-server

### Часть 5: Frontend — ГОТОВО

#### `frontend/src/views/SettingsView.vue`

Секция "Local VL Model":
- GPU Detect кнопка + отображение GPU info
- Runtime status (version, backend, server OK)
- Backend dropdown (CUDA 12.4 / CUDA 13.3 / Vulkan / CPU)
- Download кнопка + прогресс-бар
- Vision Model (.gguf) — Browse файловый диалог
- Multimodal Projector (.gguf) — Browse файловый диалог
- Embedding Dimension (number input, default 512)
- Start Server кнопка

#### `frontend/src/components/SearchBar.vue`

- Кнопка Camera (CameraIcon) справа от поиска
- Скрыта если нет VL модели (hasVL=false)
- При клике: file input с accept="image/*"
- Вызывает `search-image` event с File объектом

#### `frontend/src/App.vue`

- `hasVL` ref — проверяется при mount через `GetLLamaStatus()`
- `onSearchImage(file)` — читает файл как base64 data URI → `SearchByImageDataURI()`
- Передаёт `has-vl` prop в SearchBar

### Часть 6: main.go — ГОТОВО

```go
llamaRM := llama.NewRuntimeManager(llamaDir)
llamaSrv := llama.NewServer(llamaRM)
app := api.NewApp(d, pipeline, store, registry, thumbDir, llamaRM, llamaSrv)
wails.Run(&options.App{
    OnStartup:  app.Startup,
    OnShutdown: app.Shutdown,
    // ...
})
```

---

## Что нужно сделать дальше

### 1. Тестирование с реальной моделью

1. Скачать runtime (CUDA/Vulkan/CPU) через UI
2. Скачать SmolVLM-500M (mmproj + model .gguf)
3. Указать пути через Settings → Local VL Model
4. Нажать "Start Server"
5. Протестировать поиск по изображению

### 2. Scan Pipeline vision path (отложено)

Добавить в `internal/scan/pipeline.go` автоматическое VL-кодирование при сканировании:

```go
// В workerScan, после flushBatch для текстовых эмбеддингов:
if vlEmbedder != nil && llamaServer.IsRunning() {
    data, _ := os.ReadFile(f)
    vec, err := vlEmbedder.EmbedImage(data)
    if err == nil {
        store.Add(id, "local", "vl", vec)
        // Persist to DB...
    }
}
```

### 3. DB Migration embedding_type (отложено)

Опциональная миграция для явного разделения типов эмбеддингов:

```sql
ALTER TABLE embeddings ADD COLUMN embedding_type TEXT DEFAULT 'text';
```

Пока не нужна — VL эмбеддинги хранятся с `provider="local"`, `model_id="vl"`.

---

## Сводная таблица файлов

| Файл | Статус | Описание |
|------|--------|----------|
| `internal/gpu/detect.go` | СОЗДАН | GPU detection (nvidia-smi + WMI) |
| `internal/llama/runtime.go` | СОЗДАН | Runtime manager (download/status) |
| `internal/llama/extract.go` | СОЗДАН | Zip extraction helper |
| `internal/llama/server.go` | СОЗДАН | Server process (start/stop/PID) |
| `internal/embedder/llama_vl.go` | СОЗДАН | VL embedder (HTTP to llama-server) |
| `internal/api/app.go` | ИЗМЕНЁН | Новые API методы + Shutdown |
| `internal/models/registry.go` | ИЗМЕНЁН | RegisterLocal метод |
| `main.go` | ИЗМЕНЁН | Wiring llama + OnShutdown |
| `frontend/wailsjs/go/api/App.js` | ИЗМЕНЁН | Новые Wails bindings |
| `frontend/src/views/SettingsView.vue` | ИЗМЕНЁН | Local VL Model секция |
| `frontend/src/components/SearchBar.vue` | ИЗМЕНЁН | Camera button |
| `frontend/src/App.vue` | ИЗМЕНЁН | hasVL + onSearchImage |
| `internal/scan/pipeline.go` | НЕ ИЗМЕНЁН | Vision path отложен |
| `internal/db/db.go` | НЕ ИЗМЕНЁН | embedding_type отложен |
| `internal/db/embedding_repo.go` | НЕ ИЗМЕНЁН | filtering отложен |

---

## Рекомендации

### Выбор модели

Для начала рекомендую **SmolVLM-500M**:

- Маленький размер (~500MB)
- Быстрое кодирование
- Хорошее качество для семантического поиска

### Выбор бэкенда

- **NVIDIA GPU** → CUDA 12.4 (быстрее всего)
- **AMD/Intel GPU** → Vulkan (хорошая совместимость)
- **Нет GPU** → CPU (медленно, но работает)

### Тестирование

1. Скачать(runtime + модель)
2. Запустить llama-server
3. Протестировать кодирование одного изображения
4. Протестировать поиск по изображению
5. Замерить производительность на 100/1000/10000 изображениях
