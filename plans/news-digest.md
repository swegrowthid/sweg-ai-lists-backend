# Rencana News AI Tools Digest

## Context

- Frontend butuh data news dari blog `https://www.zainfathoni.com/blog`.
- Sumber memuat banyak seri; yang dipakai hanya seri "AI Tools Digest".
- Data disimpan di tabel `news` agar frontend tidak memanggil blog langsung.
- Sync otomatis tiap hari pukul 00:00 waktu server plus sekali saat start.
- Stack repo: Go stdlib, database/sql, goose, port Store, fixture test.
- OpenAPI di `api/openapi.yaml` adalah kontrak tunggal.

## Riset sumber

- `/blog` adalah halaman listing statis. Tanpa RSS: `/rss.xml` dan
  `/feed.xml` jawab 404.
- `/sitemap.xml` memuat 48 URL post, tapi tanpa judul: tidak bisa difilter.
- Listing memuat 48 kartu `<article>`, semuanya server-rendered.
- Kartu memuat: badge bahasa, `<h2><a href="/blog/...">judul</a></h2>`,
  `<time datetime="YYYY-MM-DD">`, dan `<p>` ringkasan.
- Judul yang memuat "AI Tools Digest" ada 32 kartu (31 ID, 1 EN).
- Listing hanya sekitar 140 KiB, jadi satu fetch cukup per sync.
- Parser memakai `golang.org/x/net/html`. Regex rapuh pada markup minified.

Call graph:

```text
GET /news
  -> news.Handler.list
    -> news.Service.List
      -> news.Store.List
        -> SELECT news ORDER BY published_at DESC

startup dan tiap 00:00 waktu server
  -> news.Scheduler.Run
    -> news.Service.Sync
      -> GET https://www.zainfathoni.com/blog
      -> parseListing filter judul "AI Tools Digest"
      -> news.Store.Upsert per url
```

## Approach

- Tabel `news`: id, title, url unik, summary, published_at, created_at,
  updated_at.
- Upsert kunci `url`. Baris dikenal di-update hanya saat kolomnya berubah,
  jadi updated_at tetap berarti "sumber terakhir berubah".
- `GET /news` publik. Tanpa endpoint tulis: daftar diisi sync saja.
- Sync: satu fetch listing, parse `<article>`, filter judul, upsert per url.
- Kartu tanpa judul, link, atau tanggal dilewati. Halaman tanpa artikel
  sama sekali = error, supaya halaman rusak tidak lolos diam-diam.
- Batas field parser mengikuti CHECK tabel, jadi parser tidak pernah kirim
  nilai yang ditolak DB.
- published_at = tengah malam UTC dari tanggal terbit kartu.
- Scheduler: sync saat start lalu tiap tengah malam lokal. Gagal sync =
  log warning, bukan fatal; run berikutnya mencoba lagi.
- Nil pool jatuh ke memory store seperti paket domain lain.

## Files to modify

- `migrations/00007_create_news.sql` - tabel news baru.
- `internal/news/news.go` - entity Entry dan UpsertInput.
- `internal/news/parser.go` - parser listing dengan x/net/html.
- `internal/news/service.go` - port Store + use case Sync dan List.
- `internal/news/store_postgres.go` - upsert ON CONFLICT url.
- `internal/news/store_memory.go` - adapter in-memory.
- `internal/news/http.go` - route GET /news.
- `internal/news/scheduler.go` - sync start + tengah malam harian.
- `internal/news/testdata/blog_listing.html` - potongan halaman asli.
- `internal/app/app.go` - wiring store, handler, dan scheduler.
- `cmd/api/main.go` dan `cmd/memserver/main.go` - jalankan scheduler.
- `api/openapi.yaml` - kontrak /news.
- `README.md` dan `deploy/README.md` - dokumentasi.

## Reuse

- Pola 4 peran paket domain: `<pkg>.go`, `service.go`, `store_*.go`, `http.go`.
- Trigger `set_updated_at()` tetap dipakai untuk updated_at.
- `internal/platform/id` tetap memasok id di memory store.
- `app.New` tetap tempat wiring graf. Nil pool tetap jatuh ke memory store.

## Steps

- [x] Tulis migration news dengan url unik dan index terbit.
- [x] Implementasikan parser listing dan filter judul.
- [x] Implementasikan service Sync dan List beserta port Store.
- [x] Implementasikan store Postgres dan memory.
- [x] Tambahkan handler GET /news.
- [x] Tambahkan scheduler start + tengah malam.
- [x] Wiring di app, cmd/api, dan cmd/memserver.
- [x] Selaraskan OpenAPI dan README.
- [x] Test parser, service, handler, dan scheduler dengan fixture asli.

## Verification

- Jalankan `go vet ./...` dan `go test ./...`.
- Jalankan `cmd/memserver` lalu panggil `GET /news`: menunggu sync start,
  cek jumlah 32, semua judul lolos filter, urut terbit terbaru dulu.
- Sync ulang: tidak ada duplikat, id dan created_at baris lama tetap.
- Sumber error: sync gagal, daftar lama tetap utuh.
