# resty-client-pool

Пул клиентов на базе resty, который **создаёт несколько независимых TCP/TLS-соединений** к одному и тому же хосту и **распределяет запросы по ним round-robin’ом**. Это помогает уйти от ситуации, когда один keep-alive канал «прилипает» к одному pod’у/инстансу за балансировщиком.

---

## Зачем это нужно

- **Один `http.Transport` ⇢ одно/несколько keep-alive соединений.** Если у вас один клиент, он может всё время ходить в один и тот же pod.
- **Пул из N клиентов** с `MaxConnsPerHost=1` у каждого даёт **N отдельных TCP/TLS-соединений**, и мы прокручиваем их **round-robin**.

---

## Конфигурация

`pkg/config/config.go`

- `BaseURL string`  
  Базовый адрес. Все пути в `Get/Post` считаются относительными от него. Можно оставить пустым и передавать абсолютные URL в каждом вызове.
- `Size int`  
  Размер пула, **сколько клиентов / TCP-соединений** создаём. По умолчанию `8`.
- `RequestTimeout time.Duration`  
  **Глобальный таймаут запроса** в resty: включает TCP-connect, TLS-handshake, отправку, ожидание заголовков и чтение тела.
- `DialTimeout time.Duration`  
  Сколько ждём **установку TCP-соединения** (3-way handshake) до падения с timeout.
- `TlsTimeout time.Duration`  
  Сколько ждём **TLS-рукопожатие** после успешного TCP-connect.
- `IdleConnTimeout time.Duration`  
  Сколько держим **keep-alive** соединение открытым, если им никто не пользуется.
- `MaxConnsPerHost int`  
  Лимит соединений на хост внутри одного `http.Transport`. В нашем пуле **ставим `1`** на каждого клиента, чтобы гарантировать по одному соединению на клиента.
- `InsecureSkipVerify bool`  
  Пропуск верификации TLS-сертификата (только для локальных тестов/стендов). **В проде держите `false`.**
- `ResponseHeaderTimeout time.Duration`  
  Сколько ждём **первые байты заголовков ответа** после полной отправки запроса. Полезно, чтобы не висеть на сервере, который «молчит».

### Почему именно такие поля

- Мы контролируем **все этапы жизненного цикла запроса**: соединение, TLS, ожидание начала ответа и глобальный дедлайн.
- Отдельный таймаут на idle помогает держать пул «здоровым» и не скапливать старые каналы.
- `MaxConnsPerHost=1` — ключ к идее пула: **1 клиент = 1 TCP-канал**. N клиентов ⇒ N каналов.

---

## Публичное API

`pkg/restypool/pool.go`

```go
type Client interface {
    Get(ctx context.Context, path string) (*resty.Response, error)
    Post(ctx context.Context, path string, body any) (*resty.Response, error)
    Close() error
}

func New(cfg config.Config) *ClientPool
```

- `New(cfg)` — создаёт пул из `cfg.Size` клиентов. Каждый клиент имеет свой `http.Transport` с `MaxConnsPerHost=1`.
- `Get/Post` — выбирают клиента **round-robin** и выполняют запрос (путь конкатенируется с `BaseURL` если он задан).
- `Close()` — idempotent; закрывает всех клиентов пула.

---

## Внутреннее устройство

### Транспорт

`pkg/restypool/client.go`:

- Создаём `*http.Transport` с:
  - `DialContext` (таймаут установки TCP),
  - `TLSHandshakeTimeout`,
  - `IdleConnTimeout`,
  - `MaxConnsPerHost: 1`,
  - `MaxIdleConnsPerHost: 1`,
  - `MaxIdleConns: Size * 2`,
  - опционально `ResponseHeaderTimeout`.
- Явно отключаем HTTP/2 через `TLSNextProto: map[string]func(string, *tls.Conn) http.RoundTripper{}` — чтобы в сценариях с L4 балансировщиком гарантировать **несколько отдельных HTTP/1.1 каналов**, а не мультиплекс на одном H2-соединении.

### Round-robin

Простейший атомарный счётчик на `atomic.Uint64`:

```
i := counter.Add(1)
idx := (i-1) % len(clients)
```

### Keep-alive

Используем стандартное поведение `http.Transport`: соединения живут и переиспользуются до `IdleConnTimeout`, что экономит RTT (нет лишних TCP+TLS рукопожатий).

---

## Тесты

`pkg/restypool/pool_test.go`:

- **Юнит-тесты на хэндлере `httptest.Server`**
  - `TestPool_Get_Post` — базовая корректность.
  - `TestPool_ContextTimeout_Get` — падение по таймауту контекста.
  - `TestPool_ContextCancel_Get` — отмена контекста.
  - `TestPool_Parallel_NoRace` — конкурентный доступ к пулу.
  - `TestPool_Close_Idempotent` — повторный `Close()` безопасен.
  - `TestPool_BaseURL_Join` — корректное склеивание `BaseURL` и `path`.
  - `TestPool_ResponseHeaderTimeout` — таймаут на заголовки.
  - `TestPool_DefaultSize` — подставляет значение по умолчанию.
  - `TestPool_DistributesAcrossConnections` — **основной тест для ТЗ**: сервер логирует `RemoteAddr`, мы убеждаемся, что запросы приходят **с разных локальных сокетов**, т.е. пул действительно держит **несколько TCP-каналов**.

Запуск:

```bash
go test ./... -v -race
```

---

## Алгоритм работы (коротко)

1. На старте создаём `Size` клиентов. У каждого — свой `http.Transport` с `MaxConnsPerHost=1`.
2. При каждом `Get/Post`:
   - выбираем индекс `i` по round-robin,
   - берём клиента `clients[i]`,
   - выполняем запрос (относительный путь приклеивается к `BaseURL`).
3. `http.Transport` сам поддерживает keep-alive и реюз соединений.
4. `Close()` закрывает всех клиентов (и их транспорты) один раз.

---

## Ограничения и заметки

- `InsecureSkipVerify=true` — **только для локальных тестов**. В продакшене — выключить.
- Если нужен универсальный интерфейс под несколько HTTP-клиентов (resty/fiber), можно вынести дженерик-интерфейс и адаптеры отдельно.

---

## Дальше по плану

- Конфиг из YAML/ENV (через `viper`).
- Универсализировать под разные HTTP-клиенты
---
