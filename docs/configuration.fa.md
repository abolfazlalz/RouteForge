# مرجع کامل فایل پیکربندی RouteForge

این سند ساختار فایل YAML مورد استفادهٔ `rforge validate`، `rforge generate` و `rforge serve` را توضیح می‌دهد. پیکربندی اصلی با `version: 1` نوشته می‌شود. قالب قدیمیِ بدون `version` نیز برای سازگاری پذیرفته می‌شود؛ جزئیات آن در بخش [سازگاری با قالب قدیمی](#سازگاری-با-قالب-قدیمی) آمده است.

## نمونهٔ کامل

```yaml
version: 1

global:
  # پورت سرورهایی که listen ندارند. پیش‌فرض 80 است.
  default_port: 8080

  # فقط در تولید Nginx استفاده می‌شود؛ native این گزینه را نادیده می‌گیرد.
  worker_connections: 1024

  # مسیر health check در native. پیش‌فرض /healthz است.
  health_path: /healthz

  proxy:
    connect_timeout: 10s
    read_timeout: 60s
    send_timeout: 60s

hot_reload:
  enabled: true
  debounce: 500ms
  poll_interval: 1s
  rollback_on_failure: true

upstreams:
  web:
    address: 127.0.0.1:3000
  api:
    address: 127.0.0.1:8081
  secure_api:
    address: https://api.internal:8443

servers:
  - name: app.example.test
    # در صورت حذف، global.default_port استفاده می‌شود.
    listen: 8088
    routes:
      - path: /api/v2
        upstream: api
        match: segment_prefix
        rewrite: /v2
        proxy_headers:
          X-Proxy-Source: routeforge
        response_headers:
          X-Content-Type-Options: nosniff
        connect_timeout: 3s
        read_timeout: 30s

      - path: /socket.io
        upstream: api
        match: segment_prefix
        websocket: true

      - path: /legacy
        upstream: web
        match: exact
        redirect_slash: true

      - path: /
        upstream: web
        match: prefix

  - name: secure.example.test
    listen: 8443
    tls:
      certificate: ./certs/server.crt
      key: ./certs/server.key
    routes:
      - path: /
        upstream: secure_api
        match: prefix
```

برای شروع native:

```sh
rforge validate --config routeforge.yaml
rforge serve --engine native --config routeforge.yaml
```

برای بررسی تنظیمات Nginx:

```sh
rforge generate --config routeforge.yaml
```

## درخت ساختار

```text
version
├── global
│   ├── default_port
│   ├── worker_connections
│   ├── health_path
│   └── proxy
│       ├── connect_timeout
│       ├── read_timeout
│       └── send_timeout
├── hot_reload
│   ├── enabled
│   ├── debounce
│   ├── poll_interval
│   └── rollback_on_failure
├── upstreams
│   └── <upstream-name>
│       └── address
└── servers[]
    ├── name
    ├── listen
    ├── tls
    │   ├── certificate
    │   └── key
    └── routes[]
        ├── path
        ├── upstream
        ├── match
        ├── websocket
        ├── redirect_slash
        ├── preserve_path
        ├── rewrite
        ├── proxy_headers
        ├── response_headers
        ├── connect_timeout
        ├── read_timeout
        └── send_timeout
```

## ریشهٔ فایل

| کلید | نوع | نیازمندی و رفتار |
| --- | --- | --- |
| `version` | عدد صحیح | در قالب نسخه‌دار اجباری است و فعلاً باید `1` باشد. |
| `global` | نگاشت | تنظیمات سراسری؛ همهٔ کلیدهایش اختیاری‌اند. |
| `hot_reload` | نگاشت | تنظیمات بازخوانی فایل؛ همهٔ کلیدهایش اختیاری‌اند. |
| `upstreams` | نگاشت نام به upstream | حداقل یک upstream لازم است. نام هر upstream در routeها ارجاع داده می‌شود. |
| `servers` | فهرست | حداقل یک سرور مجازی لازم است. |

## تنظیمات `global`

| کلید | نوع | پیش‌فرض | توضیح |
| --- | --- | --- | --- |
| `default_port` | عدد صحیح | `80` | پورت TCP برای هر `server` که `listen` آن مشخص نشده است. باید بین `1` و `65535` باشد. `listen` صریح سرور بر این مقدار اولویت دارد. |
| `worker_connections` | عدد صحیح | `1024` در generator | در فایل Nginx به `worker_connections` تبدیل می‌شود. موتور native این گزینه را مصرف نمی‌کند. |
| `health_path` | رشته | `/healthz` در native | مسیر پاسخ سلامت در native؛ روی hostهای پیکربندی‌شده پاسخ `200` و متن `ok` می‌دهد. مسیر باید با `/` شروع شود و query یا fragment نداشته باشد. Generator فعلی Nginx این endpoint را ایجاد نمی‌کند. |
| `proxy` | نگاشت | خالی | timeoutهای پیش‌فرض اتصال و پاسخ upstream؛ route می‌تواند هرکدام را جداگانه override کند. |

### `global.proxy`

هر مقدار timeout باید رشتهٔ duration قابل‌فهم برای Go باشد؛ مثل `500ms`، `10s` یا `2m`.

- `connect_timeout`: مهلت برقراری اتصال به upstream.
- `read_timeout`: تنظیم timeout خواندن پاسخ upstream.
- `send_timeout`: timeout ارسال درخواست.

معنای دقیق timeoutهای خواندن و ارسال بین دو موتور یکسان نیست. در native، `read_timeout` به مهلت دریافت response header تبدیل می‌شود؛ timeout بیکاری بین همهٔ قطعه‌های یک پاسخ streaming و `send_timeout` به‌طور کامل معادل Nginx نیستند. اگر به این رفتارها وابسته‌اید، پیش از مهاجرت بار واقعی را بررسی کنید.

## تنظیمات `hot_reload`

| کلید | نوع | پیش‌فرض | توضیح |
| --- | --- | --- | --- |
| `enabled` | boolean | `true` | فعال یا غیرفعال کردن watcher هنگام شروع process. اگر `false` باشد، فایل هنگام اجرا خودکار بازخوانی نمی‌شود. |
| `debounce` | duration | `2s` | زمان انتظار پس از تغییر فایل پیش از خواندن و اعتبارسنجی نسخهٔ جدید. |
| `poll_interval` | duration | `1s` | فاصلهٔ بررسی محتوای فایل برای پوشش ذخیره‌هایی که رویداد فایل ایجاد نمی‌کنند. |
| `rollback_on_failure` | boolean | `true` | در runtime مبتنی بر Nginx، اگر reload Nginx شکست بخورد، فایل پیکربندی فعال به نسخهٔ قبلی برگردانده می‌شود. native پیکربندی نامعتبر را فعال نمی‌کند و snapshot قبلی را نگه می‌دارد. |

Native هم از fsnotify و هم polling استفاده می‌کند. candidate جدید ابتدا parse، validate و compile می‌شود و سپس با یک جابه‌جایی اتمیک فعال می‌شود. درخواست‌هایی که پیش از reload route خود را انتخاب کرده‌اند با همان route ادامه می‌یابند. تغییر پورت، نام host یا فعال/غیرفعال شدن TLS در حین اجرا قابل اعمال نیست؛ چنین تغییری رد می‌شود و نیاز به راه‌اندازی مجدد `serve` دارد.

## تعریف upstreamها

```yaml
upstreams:
  catalog:
    address: 127.0.0.1:9000
  identity:
    address: https://identity.internal:9443
```

- کلید map نام دلخواه و یکتای upstream است؛ route باید دقیقاً به همین نام ارجاع دهد.
- برای سازگاری با Nginx، قالب پیشنهادی `host:port` است.
- Native آدرس‌های دارای scheme مثل `http://host:port` و `https://host:port` را نیز می‌پذیرد.
- کلاینت درخواست نمی‌تواند مقصد upstream را انتخاب کند؛ مقصد فقط از پیکربندی نام‌گذاری‌شده می‌آید.
- برای اجرای داخل Docker، آدرس باید از داخل container قابل دسترسی باشد. برای اجرای native، آدرس باید از میزبان قابل دسترسی باشد.

## سرورها و virtual hostها

هر عضو `servers` یک virtual host است.

| کلید | نوع | نیازمندی و رفتار |
| --- | --- | --- |
| `name` | رشته | اجباری؛ مقدار Host برای انتخاب virtual host است. برای TLS نیز باید با SNI certificate سازگار باشد. |
| `listen` | عدد صحیح | اختیاری؛ پورت این سرور. اگر حذف شود از `global.default_port` استفاده می‌شود؛ در نهایت پیش‌فرض `80` است. |
| `tls` | نگاشت | اختیاری؛ با ارائهٔ certificate و key، TLS برای listener مربوط فعال می‌شود. |
| `routes` | فهرست | routeهای این virtual host؛ هر route باید به upstream تعریف‌شده اشاره کند. |

چند `server` می‌توانند روی یک پورت باشند و با `name` از هم جدا شوند. در native، اگر TLS برای یک host روی یک پورت فعال باشد، آن listener با TLS بالا می‌آید و همهٔ hostهای همان listener باید برای استفادهٔ TLS مناسب باشند. برای listenerهای جدا، پورت جداگانه تعیین کنید. Native به‌صورت خودکار پورت `443` را انتخاب نمی‌کند؛ برای HTTPS مقدار `listen: 443` را صریح بنویسید، مگر اینکه `global.default_port` را عمداً روی `443` گذاشته باشید.

### TLS

```yaml
servers:
  - name: secure.example.test
    listen: 443
    tls:
      certificate: /etc/routeforge/tls/fullchain.pem
      key: /etc/routeforge/tls/private.key
```

هر دو مسیر لازم‌اند و باید برای process قابل خواندن باشند. Native از TLS 1.2 یا جدیدتر استفاده می‌کند و certificate را با SNI انتخاب می‌کند. در reload، فایل‌های certificate/key دوباره خوانده می‌شوند، اما تغییر port، host یا TLS mode نیاز به restart دارد. صدور خودکار certificate در دسترس نیست. مسیرهای فایل در اجرای Docker باید داخل container mount شده باشند.

## Routeها

هر route دست‌کم به `path` و `upstream` نیاز دارد.

### `path` و `upstream`

- `path` باید با `/` شروع شود.
- `upstream` نام یکی از اعضای `upstreams` است؛ آدرس مستقیم upstream داخل route نوشته نمی‌شود.
- برای جلوگیری از ابهام، از یک route مشخص برای هر رفتار استفاده کنید و routeهای تکراری با `match` و `path` یکسان تعریف نکنید.

### حالت‌های تطبیق (`match`)

اگر `match` حذف شود، مقدار `prefix` در نظر گرفته می‌شود. تقدم routeها چنین است:

1. `exact`
2. `regex`
3. `segment_prefix` با طولانی‌ترین path
4. `prefix` با طولانی‌ترین path

در حالت هم‌رتبه، ترتیب تعریف routeها حفظ می‌شود.

| مقدار | رفتار | نمونه |
| --- | --- | --- |
| `exact` | فقط خود path را می‌گیرد. | `/login` فقط `/login` را می‌گیرد. |
| `prefix` | هر مسیری را می‌گیرد که با متن path شروع شود؛ مرز segment بررسی نمی‌شود. | `/api` می‌تواند `/apix` را هم بگیرد. |
| `segment_prefix` | خود path و فرزندان آن در مرز `/` را می‌گیرد. | `/ticketing` و `/ticketing/users` را می‌گیرد، اما `/ticketing-old` را نه. |
| `regex` | تطبیق regular expression. | `^/items/[0-9]+$` |

Native با Go RE2 و به‌صورت case-insensitive تطبیق regex می‌دهد؛ lookaround و backreferenceهای PCRE پشتیبانی نمی‌شوند. Nginx از regex خودش استفاده می‌کند. برای رفتار یکسان بین موتور‌ها، regex ساده و قابل‌حمل انتخاب کنید.

برای routeهای `/ticketing` و `/ticketing/api` معمولاً `segment_prefix` مناسب است تا مسیرهای مشابهی مثل `/ticketing-old` اشتباهاً match نشوند. ترتیب طولانی‌ترین مسیر باعث می‌شود `/ticketing/api` قبل از `/ticketing` انتخاب شود.

### بازنویسی مسیر و حفظ مسیر

| کلید | نوع | پیش‌فرض | توضیح |
| --- | --- | --- | --- |
| `preserve_path` | boolean | `true` | مسیر ورودی را به upstream می‌فرستد. اگر `false` باشد، مسیر upstream به `/` تنظیم می‌شود. |
| `rewrite` | رشته | خالی | مسیر مطلقی که route به آن بازنویسی می‌شود. باید literal باشد و با `/` شروع شود؛ query و fragment در آن مجاز نیست. |

نمونهٔ حذف پیشوند:

```yaml
- path: /ticketing/api
  match: segment_prefix
  upstream: ticketing_api
  rewrite: /api
```

نتیجه:

| درخواست | مسیر upstream |
| --- | --- |
| `/ticketing/api` | `/api` |
| `/ticketing/api/users` | `/api/users` |
| `/ticketing/api/users?page=2` | `/api/users?page=2` |

Query string درخواست حفظ می‌شود. `rewrite` را با routeهای literal، ترجیحاً `segment_prefix`، استفاده کنید؛ بازنویسی routeهای regex بین native و Nginx معادل تضمین‌شده‌ای ندارد.

### اسلش انتهایی

`redirect_slash: true` درخواست دقیق route را با redirect دائمی به نسخهٔ دارای `/` می‌فرستد. برای نمونه، `path: /ticketing` مسیر `/ticketing` را به `/ticketing/` می‌فرستد. از این گزینه زمانی استفاده کنید که upstream برای اسلش انتهایی redirect معکوس نکند؛ در غیر این صورت حلقهٔ redirect ممکن است رخ دهد.

### WebSocket

```yaml
- path: /socket.io
  match: segment_prefix
  upstream: realtime
  websocket: true
```

این گزینه headerهای Upgrade را در Nginx تنظیم می‌کند. Native از قابلیت WebSocket upgrade در `httputil.ReverseProxy` استفاده می‌کند. timeoutها و خاتمهٔ اتصال‌های WebSocket را در محیط خود بررسی کنید؛ اتصال طولانی‌مدت می‌تواند بعد از reload نیز باز بماند.

### Headerها

```yaml
proxy_headers:
  X-Route-Name: api
response_headers:
  X-Content-Type-Options: nosniff
```

- `proxy_headers` headerهای ارسالی به upstream را تنظیم یا جایگزین می‌کند.
- `response_headers` headerهای پاسخ upstream را تنظیم یا جایگزین می‌کند.
- نام header فقط می‌تواند شامل حروف ASCII، عدد و خط تیره باشد.
- مقدار header نباید newline، `;`، `{` یا `}` داشته باشد.
- مقادیر ثابت یا environment substitution استفاده کنید؛ این فیلدها برای درج directiveهای دلخواه Nginx نیستند.
- Native headerهای ورودی `Forwarded` و `X-Forwarded-*` را پیش از ساخت مقادیر proxy خودش پاک می‌کند تا مقدار جعلی کلاینت به‌عنوان اطلاعات قابل‌اعتماد forward نشود. مقادیر تعریف‌شده در `proxy_headers` پس از آن اعمال می‌شوند.

### Timeoutهای route

هر route می‌تواند `connect_timeout`، `read_timeout` و `send_timeout` داشته باشد. این مقادیر بر timeoutهای `global.proxy` اولویت دارند. قالب مقادیر مثل `2s`، `250ms` یا `1m` است. خالی گذاشتن هر مقدار یعنی استفاده از مقدار global یا پیش‌فرض موتور.

## Environment variables

مقادیر `${NAME}` و `$NAME` پیش از parse شدن YAML با environment جایگزین می‌شوند. متغیر استفاده‌شده باید set شده باشد؛ مقدار خالی مجاز است اگر خود متغیر وجود داشته باشد.

```yaml
upstreams:
  app:
    address: ${APP_HOST}:${APP_PORT}
servers:
  - name: ${PUBLIC_HOST}
```

جایگزینی قبل از YAML parsing انجام می‌شود. اگر مقدار متغیر شامل `:`, `#`, `: ` یا نویسه‌های ویژهٔ YAML است، آن را با روش مناسب برای YAML quote کنید یا از مقادیر ساده استفاده کنید. Secretها را در فایل‌های عمومی یا log قرار ندهید؛ فایل config و keyهای TLS را با دسترسی محدود نگه دارید.

## سازگاری با قالب قدیمی

فایل‌های قدیمی بدون `version` که upstreamها را به‌صورت رشته و routeها را با `prefix: true` می‌نویسند پذیرفته می‌شوند:

```yaml
upstreams:
  frontend: localhost:3000
  ticketing: localhost:3001
servers:
  - name: app.local
    routes:
      - path: /ticketing
        upstream: ticketing
        prefix: true
        redirect_slash: true
      - path: /
        upstream: frontend
```

Loader این فایل را به schema version 1 تبدیل می‌کند. `prefix: true` به `match: segment_prefix` تبدیل می‌شود. برای پیکربندی جدید از `version: 1` و upstreamهای دارای `address` استفاده کنید. در فایل version 1 کلید ناشناخته، کلید تکراری و فیلد route قدیمی `prefix` خطا هستند.

## تفاوت موتور‌ها و نکات مهاجرت

- **Native:** پورت‌ها را مستقیماً روی میزبان باز می‌کند، `health_path` را ارائه می‌دهد و config را در حافظه reload می‌کند. `worker_connections` در آن کاربرد ندارد.
- **Nginx:** `rforge generate` خروجی Nginx می‌سازد و فرمان‌های `up/down/restart/status/logs/reload` همان Docker Compose فعلی را مدیریت می‌کنند. گزینهٔ `global.health_path` در generator Nginx endpoint ایجاد نمی‌کند.
- Timeoutهای Go و Nginx در تمام حالت‌ها معادل نیستند؛ بخش timeout را پیش از مهاجرت production مرور کنید.
- `listen`، TLS و virtual hostها را در حین اجرای native تغییر ندهید؛ process را با فایل جدید restart کنید.
- بعد از تغییر schema یا استفاده از نسخهٔ جدید RouteForge، باینری را rebuild کنید تا parser جدید اجرا شود.

## خطاهای رایج

- **upstream تعریف نشده:** مقدار `routes[].upstream` باید نام موجود در `upstreams` باشد.
- **route اشتباه انتخاب می‌شود:** `match` و تقدم exact/regex/segment-prefix/prefix را بررسی کنید؛ برای مرزبندی URL از `segment_prefix` استفاده کنید.
- **پورت باز نمی‌شود:** بررسی کنید پورت معتبر و آزاد باشد و process اجازهٔ bind آن را داشته باشد. پورت‌های پایین مثل 80 و 443 ممکن است به دسترسی مدیریتی نیاز داشته باشند.
- **TLS certificate بارگذاری نمی‌شود:** مسیرها، مجوز خواندن فایل، جفت‌بودن certificate/key و SNI برابر `servers[].name` را بررسی کنید.
- **`field ... not found`:** باینری قدیمی یا کلید اشتباه است. باینری جدید بسازید و نام کلید را با این مرجع مقایسه کنید.
- **فایل unversioned رد می‌شود:** قالب قدیمی فقط scalar upstream و فیلدهای شناخته‌شده را می‌پذیرد. برای مهاجرت، `version: 1` اضافه کنید، upstreamها را به `address` تبدیل و `prefix: true` را به `match: segment_prefix` تبدیل کنید.
