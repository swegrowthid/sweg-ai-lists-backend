# Rencana Skema Post Setting AI Coding Agent

## Context

- Pengguna butuh skema post untuk setting AI dari coding agent.
- Setiap post butuh judul dan isi fleksibel.
- Isi bisa campur: link rekomendasi, file .md, atau text area.
- Satu post bisa pakai banyak category. Contoh category adalah coding agent.
- Tanpa tabel tags terpisah.
- File .md kecil disimpan inline di DB. Tanpa CDN dulu.
- Hanya user terdaftar boleh tulis. Langsung published.
- Stack repo: Go stdlib, database/sql, goose, port user.Store.
- OpenAPI di api/openapi.yaml adalah kontrak tunggal.

## Riset Exa

- Pola umum: tabel posts + categories + join many-to-many.
- Pola isi campur: satu tabel items dengan kolom kind dan position.
- Hybrid umum: teks kecil di DB, file besar via URL CDN.
- Opsi CDN gratis untuk cadangan:
  - Cloudflare R2: 10 GB, nol egress.
  - jsDelivr + repo GitHub publik: bandwidth tanpa batas.
  - Batas jsDelivr: satu file 20 MB.
  - Backblaze B2: 10 GB gratis.
- Keputusan awal: kolom cdn_url nullable sebagai cadangan. Tidak dipakai di v1.

Sumber:

- https://databasesample.com/database/ai-prompts-database-database
- https://dev.to/gowrishankar_rangasamy_f9/how-i-built-a-free-ai-prompt-library-with-nodejs-and-postgresql-from-zero-to-live-in-30-days-2n75
- https://github.com/michaelschecht/my-prompt-library
- https://agentdeals.dev/storage-alternatives
- https://www.jsdelivr.com/
- https://eastondev.com/blog/en/posts/dev/20251130-r2-picgo-setup/

Call graph:

```text
POST /posts
  -> post.Handler.create
    -> decode and validate CreatePostRequest
      -> post.Service.Create
        -> post.Store.Create
          -> INSERT posts + post_categories + post_items
GET /posts?category=slug&q=kata
  -> post.Handler.list
    -> post.Service.List
      -> post.Store.List
        -> SELECT posts JOIN post_categories JOIN categories
GET /posts/{slug}
  -> post.Handler.get
    -> post.Service.Get
      -> post.Store.FindBySlug
        -> SELECT post + categories + items ordered by position
```

## Approach

- Buat tabel categories dengan slug unik untuk pencarian.
- Buat tabel posts dengan author_id ke users(id).
- Tanpa kolom status. Semua post langsung published.
- Buat join post_categories dengan PK komposit.
- Buat satu tabel post_items untuk semua isi campur.
- Kolom kind: markdown, text, link, file.
- Kolom body_text untuk markdown, text, dan file inline.
- Kolom url hanya untuk kind link.
- Kolom filename dan mime hanya untuk kind file.
- Kolom position untuk urutan tampil.
- Kolom cdn_url dan cdn_provider nullable untuk update nanti.
- Tambah slug unik di posts untuk URL cantik.
- Tambah index untuk join, slug, dan pencarian ILIKE.
- Wajib auth Bearer pada POST. Ikuti pola GET /users.
- Ikuti pola repo: domain, service, store_postgres, store_memory, handler.

Skema inti:

```text
categories (1)
  -> post_categories (N)
    -> posts (1)
      -> post_items (N)
```

## Files to modify

- `migrations/00003_create_categories.sql` - tabel categories baru.
- `migrations/00004_create_posts.sql` - tabel posts, post_categories, post_items baru.
- `internal/post/post.go` - entity Category, Post, PostItem, error domain.
- `internal/post/service.go` - use case create dan list dengan validasi kind.
- `internal/post/store_postgres.go` - query parameterized dan translate error 23505 dan 23503.
- `internal/post/store_memory.go` - memory store untuk test dan mode tanpa DB.
- `internal/post/http.go` - route, decode, validasi, auth, status code.
- `internal/app/app.go` - wiring store dan handler seperti user dan auth.
- `api/openapi.yaml` - kontrak categories dan posts.
- `plans/ai-posts-schema.md` - file rencana ini.

## Reuse

- `user.Store` tetap menjadi pola port persistence.
- `user.PostgresStore` tetap menjadi pola translate PgError 23505.
- `auth.Middleware.RequireAuth` tetap menjadi penjaga route tulis.
- `internal/platform/db` tetap memasok pool DB.
- `internal/app/app.go` tetap menjadi tempat wiring graph.
- Migration tetap menjadi sumber bentuk kolom dan constraint.
- Trigger `set_updated_at()` tetap dipakai untuk updated_at.

## Steps

- [x] Tulis migration categories dengan slug unik.
- [x] Tulis migration posts dengan FK author ke users.
- [x] Tulis migration post_categories dengan PK komposit dan cascade.
- [x] Tulis migration post_items dengan check kind dan position.
- [x] Tambah index join, slug, dan search.
- [x] Implementasikan domain post, category, dan item.
- [x] Implementasikan service dan validasi kind dan position.
- [x] Implementasikan Postgres store dan memory store.
- [x] Tambahkan handler dan route dengan auth tulis.
- [x] Selaraskan OpenAPI.
- [x] Tambah test create, duplicate slug, FK invalid, dan list per category.

## Verification

- Jalankan `go test ./...`.
- Jalankan `go vet ./...`.
- Jalankan `goose up` di DB dev.
- Test POST category lalu POST post dengan auth.
- Test POST tanpa token wajib 401.
- Test GET list filter category.
- Test GET detail slug memuat categories dan items urut.
- Test item link plus link tersimpan semua.
- Test file .md inline tersimpan di body_text.
