# http-client-pool (resty + fiber)

Пулы HTTP-клиентов для **распараллеливания запросов на один и тот же хост**.  
Идея: создать **N независимых TCP/TLS-соединений** и раздавать запросы по ним **round-robin** — чтобы не «прилипать» одним keep-alive к одному pod’у/инстансу за балансировщиком.

Поддержаны 2 клиента:
- **Resty** (`net/http`)
- **Fiber client** (`fasthttp`)

---

## Зачем это нужно

- У одного клиента (`http.Transport`) обычно **несколько keep-alive коннектов**, но в реальности часто «залипаем» на одном и том же pod’е.
- **Пул из N клиентов** с `MaxConnsPerHost=1` у каждого даёт **минимум N отдельных TCP/TLS-каналов**, а RR равномерно распределяет запросы.
- Это снижает риск «перегреть» один pod и помогает балансировке.

---

## Конфигурация

`pkg/config/config.go`

- `BaseURL string` — Базовый URL. Пути в `Get`/`Post` считаются относительными. Можно оставить пустым и передавать абсолютные URL.
- `Size int` — Размер пула (**сколько клиентов/соединений** создаём). По умолчанию `8`.
- `RequestTimeout time.Duration`   
  **Глобальный таймаут запроса**:
  - Для **Resty** — от начала до конца: TCP connect, TLS, отправка, ожидание заголовков, чтение тела (необязательный,контролируется `ctx`).
  - Для **Fiber** — применяется как `ReadTimeout`/`WriteTimeout`/`MaxConnWaitTimeout` (`ctx` нет).
- `DialTimeout time.Duration` — Сколько ждём **установку TCP** (3-way handshake).
- `TlsTimeout time.Duration` — Сколько ждём **TLS-рукопожатие** (только Resty/`net/http`).
- `IdleConnTimeout time.Duration` — Сколько держим **idle (keep-alive)** соединение, если им никто не пользуется.
- `MaxConnsPerHost int` — Лимит соединений на хост **внутри одного клиента**. В пуле ставим `1`, чтобы гарантировать **1 клиент = 1 коннект**.
- `InsecureSkipVerify bool` — Пропуск проверки TLS-серта (**только для тестов/локалки**).
- `ResponseHeaderTimeout time.Duration` — Сколько ждём **первые байты заголовков ответа** (только Resty/`net/http`).

---

## Публичный интерфейс (общий)

`pkg/pool/client.go`

```go
type Client interface {
    Get(ctx context.Context, path string) (Response, error)
    Post(ctx context.Context, path string, body any) (Response, error)
    Close()
}

type Response interface {
    StatusCode() int
    Body() []byte
}
```

---

## Таймлайн запроса

### Resty (`net/http`)

**Холодный запрос:**
1. TCP connect — `DialTimeout` + общий `RequestTimeout`
2. TLS handshake — `TlsTimeout` + общий `RequestTimeout`
3. Запись запроса — общий `RequestTimeout`
4. Ожидание заголовков — `ResponseHeaderTimeout` + общий `RequestTimeout`
5. Чтение тела — общий `RequestTimeout`
6. Idle — соединение остаётся до `IdleConnTimeout`

**Keep-alive:** шаги 1-2 пропускаются.

### Fiber (`fasthttp`)

- TCP connect — `DialTimeout`
- TLS — в рамках `RequestTimeout` (через Read/WriteTimeout)
- Idle — управляется `MaxIdleConnDuration`
- Нет ResponseHeaderTimeout и per-request context



---

## Тесты

Есть два набора:

- `pkg/restypool/pool_test.go` — юнит-тесты Resty-пула
- `pkg/pool/client_suite_test.go` — общий suite для Resty и Fiber (учитывает различия в поддержке контекста и ResponseHeaderTimeout)

---

## Бенчмарки

Результаты (M4, macOS):

```
BenchmarkPools_Small
BenchmarkPools_Small/resty/small-10         	   30957	     38678 ns/op	    8453 B/op	      94 allocs/op
BenchmarkPools_Small/fiber/small-10         	   31162	     38616 ns/op	    5388 B/op	      78 allocs/op

BenchmarkPools_Large
BenchmarkPools_Large/resty/large-10         	    2817	    389401 ns/op	 2141373 B/op	     130 allocs/op
BenchmarkPools_Large/fiber/large-10         	    2996	    350101 ns/op	 1329366 B/op	     102 allocs/op
```

**Выводы:**
- На маленьких ответах Resty и Fiber примерно одинаковые по скорости.
- Fiber требует меньше памяти и делает меньше аллокаций.
- На больших ответах Fiber быстрее и экономичнее.

---

## Ограничения

- Fiber-пул не поддерживает отмену per-request `ctx`.
- Resty умеет всё (`ctx`, ResponseHeaderTimeout).