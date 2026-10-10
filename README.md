# sweg-ai-lists-backend

Backend komunitas sweg-ai. Go stdlib + PostgreSQL (Supabase via `DB_URL`).

- Modul: `github.com/swegrowthid/sweg-ai-lists-backend`
- Stack: stdlib `net/http`, `database/sql` + `pgx/v5/stdlib`, goose v3, JWT HS256, Scalar docs
- Kontrak: `api/openapi.yaml` adalah sumber tunggal. Jangan ubah kontrak tanpa ubah kode.

## Arsitektur

Setiap paket domain ikut pola 4 peran:

```text
<pkg>.go          bentuk + error bertag
service.go        use case + port Store
store_postgres.go adapter Postgres (produksi)
store_memory.go   adapter in-memory (test, dev)
http.go           batas HTTP + mapping error ke status
```

Paket domain: `user`, `auth`, `post`, `news`, `tools`, `dailyterm`, `wanted`. Paket `health` hanya service + http.
Paket platform: `config`, `db`, `httpserver`, `logger`, `docs`, `id`, `validator`.
Wiring graf ada di `internal/app/app.go`. `main` hanya bangun resource lalu jalan.

```text
mux -> health.Handler -> health.Service -> db.Pinger
mux -> user.Handler -> user.Service -> user.Store
mux -> auth.Handler -> auth.Service -> user.Service + tokens + RefreshStore
mux -> post.Handler -> post.Service -> post.Store
mux -> news.Handler -> news.Service -> news.Store
mux -> tools.Handler -> tools.Service -> tools.Store
mux -> dailyterm.Handler -> dailyterm.Service -> dailyterm.Store
mux -> wanted.Handler -> wanted.Service -> wanted.Store + post.Service
```

Nil pool jatuh ke memory store. Itu dipakai test dan `cmd/memserver`.
Middleware: log, lalu CORS, lalu route. Tulis butuh Bearer token.
Sync news, tools, dan daily term jalan di goroutine sendiri: sekali saat start, lalu tiap 00:00 waktu server.

## Struktur

```text
cmd/api        binary API produksi (butuh DB)
cmd/migrate    binary goose (up, down, status)
cmd/memserver  server scratch in-memory, untuk E2E lokal
internal/app       wiring graf
internal/user      register + list user
internal/auth      login, refresh, logout, middleware JWT
internal/post      categories + posts
internal/news      AI Tools Digest dari blog zainfathoni.com
internal/tools     katalog tools AI dari markdown tools-ai-swe-growth
internal/dailyterm jurnal daily term dari markdown daily-term-SE-Growth
internal/wanted    daftar wanted publik untuk post, tanpa login
internal/health    liveness + readiness
internal/platform  config, db, httpserver, logger, docs, id, validator
api/openapi.yaml   kontrak API
migrations/        skema goose, additif saja
deploy/            unit systemd + panduan server
plans/             dokumen keputusan
```

## Mulai

Isi `.env` dari `.env-example`. `DB_URL` dan `JWT_SECRET` wajib.

```sh
make migrate-status
make migrate-up
make run
```

Perintah lain:

```sh
make vet
make test
make build
make build-linux
```

Server scratch tanpa DB:

```sh
go run ./cmd/memserver
```

Server jalan di `127.0.0.1:8790`. Data hilang saat proses mati.

## Konfigurasi

| Key | Default | Arti |
| --- | --- | --- |
| `DB_URL` | kosong | URL Postgres penuh. Kosong = boot gagal. |
| `JWT_SECRET` | kosong | Secret HS256. Kosong = boot panic. |
| `JWT_ACCESS_TTL` | `24h` | Umur access token. |
| `JWT_REFRESH_TTL` | `168h` | Umur refresh token. |
| `APP_ADDR` | `:8080` | Alamat listen. |
| `APP_ENV` | `dev` | `prod` mematikan `/docs` kecuali `DOCS_UI=1`. |
| `APP_VERSION` | `dev` | Versi di health response. Deploy isi dari git SHA. |
| `APP_SERVICE` | `sweg-ai-lists-backend` | Nama service di log. |
| `APP_LOG_LEVEL` | kosong | Level log. |
| `DOCS_UI` | on di luar prod | `1` paksa on, `0` paksa off. |
| `CORS_ORIGINS` | kosong | Daftar origin browser, koma-pisah. Kosong = tanpa header CORS. |

Prod butuh `CORS_ORIGINS=https://ai-sweg.my.id` agar frontend statis bisa panggil API dari browser.

## Endpoint

| Method | Path | Auth | Arti |
| --- | --- | --- | --- |
| `GET` | `/healthz` | tidak | Liveness. |
| `GET` | `/readyz` | tidak | Readiness. 503 saat DB down. |
| `POST` | `/users/register` | tidak | Buat user. Tanpa token keluar. |
| `GET` | `/users` | ya | Daftar user. |
| `PUT` | `/users/password` | ya | Ganti password sendiri. Butuh `current_password` + `new_password`. Sukses cabut semua refresh token. |
| `POST` | `/auth/login` | tidak | Identifier terima username atau email. |
| `POST` | `/auth/refresh` | tidak | Rotasi refresh token. Token lama mati. |
| `POST` | `/auth/logout` | tidak | Cabut refresh token. Jawab 204. |
| `GET` | `/categories` | tidak | Daftar category, induk dulu. |
| `POST` | `/categories` | ya | Buat category atau derivative. |
| `GET` | `/posts` | tidak | Daftar post, tanpa items. Filter `?category=` dan `?q=`. |
| `POST` | `/posts` | ya | Buat post. Langsung published. |
| `GET` | `/posts/{slug}` | tidak | Detail post dengan items urut position. |
| `GET` | `/wanted` | tidak | Daftar post wanted, tanpa items, terbaru ditandai dulu. |
| `POST` | `/wanted` | tidak | Tandai post wanted. Duplikat idempotent jawab 200. |
| `DELETE` | `/wanted/{slug}` | tidak | Hapus post dari wanted. Jawab 204. |
| `GET` | `/news` | tidak | Daftar news AI Tools Digest, terbit terbaru dulu. |
| `GET` | `/tools` | tidak | Daftar tools AI urut katalog. Filter `?category=` dan `?q=`. |
| `GET` | `/tools/categories` | tidak | Mapping kategori tools: slug, prefix, file sumber, count. |
| `GET` | `/tools/{id}` | tidak | Detail satu tool, id `P-001`, `CA-018`, `ADE-012`. |
| `GET` | `/daily-terms` | tidak | Satu bulan daily term. Filter `?month=YYYY-MM`. |
| `GET` | `/daily-terms/months` | tidak | Bulan daily term yang tersedia: month_key, nama, file sumber, count. |
| `GET` | `/daily-terms/{id}` | tidak | Detail satu term, id `2026-01-sieve-algorithm-or-sieve-of-eratosthenes`. |
| `GET` | `/docs` | tidak | UI Scalar. Mati di prod kecuali `DOCS_UI=1`. |
| `GET` | `/docs/openapi.yaml` | tidak | Spec. Selalu on. |

Body JSON dibatasi 1 MiB. Field tak dikenal ditolak. Body harus satu objek JSON.

## Aturan category dan post

Category adalah tree dua level. `parent_id` NULL = induk. Terisi = derivative.
Seed bawaan: `coding`, `creative`, `presentation` (semua induk).

- `POST /posts` terima `category` (wajib, slug induk) dan `derivative` (opsional, anak langsung dari induk).
- `category` tidak boleh derivative. Derivative harus anak dari category yang sama.
- `?category=<slug>` ikut cocokkan turunan. Filter induk temukan post berderivative.
- Slug: huruf kecil, angka, tanda hubung. Maks 100. Title maks 200. Name maks 100.
- Items: 1 sampai 20. Urutan array jadi urutan tampil. Client tidak kirim position.
- `body_text` maks 64 KiB per item. Simpan byte-exact, tanpa trim.
- Satu kind = satu bentuk payload. Field milik kind lain ditolak walau kosong. `null` berarti absen dan boleh.
- `kind=link`: hanya `url`. Wajib `http` atau `https` plus host.
- `kind=markdown` atau `text`: hanya `body_text`.
- `kind=file`: `body_text` + `filename`. `mime` opsional, default `text/markdown`.
- Byte NUL ditolak di semua field teks.
- Slug post bentrok jawab 409. Category tak dikenal jawab 400 `unknown category`.

Contoh minimal:

```json
{
  "slug": "first-post",
  "title": "First Post",
  "category": "coding",
  "items": [
    { "kind": "link", "url": "https://example.com/docs" }
  ]
}
```

Tiga teks 400 menunjuk lapisan gagal:

| Body | Lapisan |
| --- | --- |
| `invalid request body` | Decode JSON: key asing, JSON rusak, body > 1 MiB. |
| `invalid input` | Validasi service: slug, title, bentuk item, URL, batas. |
| `unknown category` | Lookup store: slug category atau derivative tidak ada. |

## News

`GET /news` menyajikan daftar post "AI Tools Digest" dari blog
`https://www.zainfathoni.com/blog`. Tabel `news` menyimpan satu baris per post,
kuncinya `url` unik, jadi sync ulang tidak pernah menggandakan baris.

Sync jalan di dalam proses API: sekali saat start, lalu tiap hari pukul 00:00
waktu server. Endpoint tidak pernah memanggil blog. Kalau sync gagal, hasil
terakhir tetap tersaji dan run berikutnya mencoba lagi.

- Filter: judul kartu listing memuat "AI Tools Digest", tanpa peduli besar-kecil huruf.
- `published_at` = tengah malam UTC tanggal terbit di kartu listing.
- Daftar urut terbit terbaru dulu; `summary` boleh string kosong.
- Sumber atau filter berubah = ubah `internal/news/service.go`, bukan skema.

## Tools

`GET /tools` menyajikan katalog tools AI dari repo
`github.com/swegrowthid/tools-ai-swe-growth`. Tiga file markdown di repo itu
adalah databasenya: `providers.md`, `codingagents.md`, `ade.md`. Tidak ada
tabel SQL untuk katalog - parser membaca tabel markdown dan menyimpan hasilnya
sebagai snapshot in-memory, lalu sync menggantinya atomik.

Sync berjalan di dalam proses API: sekali saat start, lalu tiap hari pukul
00:00 waktu server. Endpoint tidak pernah memanggil GitHub. Kalau sync gagal,
snapshot terakhir tetap tersaji dan run berikutnya mencoba lagi.

- `GET /tools/categories` menjawab mapping: `providers` (prefix `P`,
  `providers.md`), `coding-agents` (`CA`, `codingagents.md`), `ade` (`ADE`,
  `ade.md`), plus `count` baris hasil sync terakhir.
- `?category=` terima slug atau prefix, case-insensitive: `providers` = `p`.
- `?q=` cocokkan substring nama tool, case-insensitive.
- Field per kategori: providers membawa `top_up`, `subscribe`, `min_spend`;
  ade membawa `ai_features`. Field milik kategori lain tidak muncul di JSON.
- `status` dinormalisasi lowercase tanpa emoji; `updated` jadi `YYYY-MM-DD`.
- Satu baris rusak tidak membatalkan file; file tanpa tabel ID membatalkan
  seluruh sync sehingga kategori tidak pernah kosong separuh.
- Sumber atau kolom berubah = ubah `internal/tools/parser.go`, bukan skema.

## Daily Term

`GET /daily-terms` menyajikan jurnal daily term dari repo
`github.com/swegrowthid/daily-term-SE-Growth`. Repo itu menyimpan satu berkas
markdown per bulan di bawah direktori tahun (`2026/jan-term.md`). Tidak ada tabel
SQL: parser membaca judul H2 (juga H3, karena dua term sumber ada satu tingkat
lebih dalam), memisahkan bagian `### Definition:` dan `### Example:`, lalu
menyimpan hasilnya sebagai snapshot in-memory yang diganti atomik saat sync.

Sync berjalan di dalam proses API, satu slot dengan news dan tools: sekali saat
start, lalu tiap hari pukul 00:00 waktu server. Endpoint tidak pernah memanggil
GitHub. Kalau sync gagal, snapshot terakhir tetap tersaji dan run berikutnya
mencoba lagi.

- `?month=YYYY-MM` memilih bulan. Bulan dengan format salah jawab 400
  `invalid month`; bulan yang formatnya benar tapi tidak punya berkas jawab 404
  `month not found`.
- Tanpa `?month`, server memakai bulan server sekarang bila berkasnya ada, dan
  kalau belum ada memakai bulan terbaru yang tersedia. `meta.month_key` di
  response mengatakan bulan mana yang akhirnya disajikan.
- `GET /daily-terms/months` menjawab daftar bulan terbaru dulu, jadi frontend
  bisa membangun pemilih bulan tanpa menyalin tata letak repo sumber.
- `definition` dan `example` adalah markdown mentah. Backend tidak merender
  markdown; frontend yang merender. Label `### Definition:` dibuang dari teks.
- `day` bukan tanggal kalender. Sumber menomori hari kerja tiap bulan, jadi
  angkanya bisa melompati akhir pekan, bisa berulang, dan bisa mulai dari 28.
- Daftar bulan ditemukan lewat listing git tree, bukan daftar tetap: upstream
  memakai nama bulan yang tidak konsisten (`june` dan `july`, `sept` dan
  `october`) dan tidak punya berkas untuk semua bulan, jadi bulan yang tidak
  dikenal dilewati tanpa membatalkan sync.
- Satu berkas gagal, atau listing tidak menemukan bulan sama sekali, membatalkan
  seluruh sync sehingga tidak ada bulan yang tersaji separuh.
- Sumber atau judul berubah = ubah `internal/dailyterm/parser.go`, bukan skema.

## Wanted

`GET /wanted` menyajikan daftar wanted publik untuk post: satu baris per
post yang ditandai wanted, tanpa login. Tabel `wanted_posts` menyimpan satu
baris per `post_id`, jadi tandai ulang tidak pernah menggandakan baris.

- `POST /wanted` terima `{"slug":"..."}`. Baris baru jawab 201 dengan post
  bentuk list (tanpa items, untuk kartu). Slug yang sudah wanted jawab 200
  dengan post yang sama: idempotent, bukan 409.
- `GET /wanted` jawab array post bentuk list, paling baru ditandai dulu.
- `DELETE /wanted/{slug}` cabut tanda. Jawab 204. Post tak dikenal jawab
  404 `post not found`; post yang tidak wanted jawab 404 `wanted entry not found`.
- Hapus post hapus baris wanted-nya lewat `ON DELETE CASCADE`.

## Auth

Login pakai username ATAU email. Email case-insensitive. Salah identifier dan salah password jawab 401 generik yang sama.

Refresh token bawa `jti`. Rotasi bersifat atomik. Token lama yang dipakai ulang membunuh seluruh keluarga token user itu. Logout cabut token itu.

## Migrasi

Goose melacak versi di `goose_db_version`. Migrasi lama tidak boleh ditulis ulang. Semua perubahan skema lewat file baru.

| File | Isi |
| --- | --- |
| `00001_create_users.sql` | Tabel `users` + trigger `set_updated_at`. |
| `00002_create_refresh_tokens.sql` | Tabel `refresh_tokens`. |
| `00003_create_categories.sql` | Tabel `categories`. Slug unik + regex. |
| `00004_create_posts.sql` | `posts`, `post_categories`, `post_items`. Index trigram judul. |
| `00005_add_category_parent.sql` | Kolom `parent_id` + trigger batas depth 2. |
| `00006_seed_default_categories.sql` | Seed `coding`, `creative`, `presentation`. Idempoten. |
| `00007_create_news.sql` | Tabel `news`. Kunci unik `url`. |
| `00008_create_wanted_posts.sql` | Tabel `wanted_posts`. Kunci utama `post_id`, hapus post ikut hapus baris. |

Jangan pakai pooler transaksi port 6543 untuk migrasi. Pakai port 5432. Lihat panduan deploy.

## Test

```sh
go vet ./...
go test ./...
```

Test pakai store in-memory. Tidak ada test menyentuh DB. `internal/app/app_test.go` tukar semua store ke memory dan tempuh register, login, buat category, buat post, baca detail, list.

Test `internal/news` memakai potongan halaman listing asli di `internal/news/testdata` dan server `httptest`: tanpa jaringan, tanpa DB.

Test `internal/dailyterm` memakai potongan berkas bulan asli di `internal/dailyterm/testdata` dan server `httptest` untuk listing tree dan berkas bulan: tanpa jaringan, tanpa DB.

## Deploy

Lihat `deploy/README.md`. Ringkas alur:

1. Build biner linux. Rsync biner + `migrations/` + `release.env` ke server.
2. Jalankan `sweg-ai-migrate.service` (goose up).
3. Restart `sweg-ai.service`.
4. Cek `http://127.0.0.1:8080/readyz`.

Migrasi gagal = API lama tetap jalan. Secrets GitHub: `DEPLOY_HOST`, `DEPLOY_USER`, `DEPLOY_SSH_KEY` wajib. `DEPLOY_PORT`, `DEPLOY_PATH` opsional.

Sync news, tools, dan daily term jalan di dalam biner API, tanpa unit systemd tambahan: sekali saat start lalu tiap 00:00 waktu server.

CI jalan di tiap push dan PR: vet, test, build linux. Job deploy hanya push `main`.
