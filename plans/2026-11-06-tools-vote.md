# Plan: Vote untuk Tools yang Dipakai

Tanggal: 2026-11-06
Revisi: 2026-11-07 - struktur dirapikan, keputusan auth baca dan
persiapan komentar ditambahkan
Status: Draft desain - implementasi belum dimulai
Scope: backend (`sweg-ai-lists-backend`)

## Ringkasan

Tambahkan fitur vote pada katalog tools AI: satu user terautentikasi boleh
menyatakan "saya memakai tool ini" satu kali per tool. Vote disimpan di
Postgres, sedangkan katalog tetap bersumber dari tiga file markdown
`tools-ai-swe-growth` dan tetap berupa snapshot in-memory.

Hasil akhir:

- `POST /tools/{id}/votes` menambah vote user yang sedang login.
- `DELETE /tools/{id}/votes` mencabut vote user tersebut.
- `GET /tools` dan `GET /tools/{id}` membawa `votes` (jumlah vote) dan
  `voted` (apakah user yang meminta sudah vote).
- `GET /tools?sort=votes&order=desc` mengurutkan tool berdasarkan vote.
- Tidak ada downvote, rating bintang, atau vote anonim di v1.

## Keputusan yang sudah dikonfirmasi

Tiga hal yang sudah disepakati sebelum implementasi:

1. **Tulis vote wajib auth, baca hasil vote publik.** `POST`/`DELETE
   /tools/{id}/votes` tetap di balik `RequireAuth`. `GET /tools` dan
   `GET /tools/{id}` tidak pernah menolak request karena token: `votes` selalu
   dikirim, `voted` bernilai `false` saat token absen, invalid, atau expired.
   Token valid hanya dipakai untuk mengisi `voted` milik viewer.
2. **Katalog tools bersumber dari repo, bukan database.** Tiga file markdown
   di `tools-ai-swe-growth` di-fetch saat start dan tiap tengah malam, lalu
   disimpan sebagai snapshot in-memory. Tidak ada tabel `tools` di migrasi.
   Vote (dan nanti komentar) adalah data SQL terpisah yang memakai `tool_id`
   sebagai natural key tanpa foreign key ke katalog.
3. **Komentar tools memungkinkan tanpa mengubah arsitektur katalog.** Desain
   lengkap ada di bagian "Persiapan komentar". Tidak ada migration komentar di
   tahap vote ini; migration berikutnya baru dibuat saat fitur komentar
   disetujui.

Dokumen ini masih berupa desain. Belum ada kode, route, atau migration yang
ditulis; semua langkah implementasi di bawah belum dikerjakan.

## Konteks repo

- `internal/tools/tools.go` mendefinisikan `Tool`, `Store`, dan error domain.
  Komentar paket menyatakan katalog tidak punya tabel SQL; markdown adalah
  sumber kebenaran.
- `internal/tools/service.go` memiliki `Store` snapshot read-only:
  `ReplaceAll`, `List`, `Get`. Sync harian mengganti seluruh snapshot.
- `internal/tools/http.go` mendaftarkan `GET /tools`,
  `GET /tools/categories`, dan `GET /tools/{id}`; semuanya publik.
- `internal/app/app.go` selalu memakai `tools.NewMemoryStore()` untuk katalog.
  Saat `pool != nil`, store domain lain memakai Postgres; tools tidak.
- `internal/auth/middleware.go` punya `RequireAuth` berbasis Bearer JWT dan
  menaruh `auth.Principal` di context.
- `internal/auth/auth.go` menyediakan `PrincipalFrom(ctx)`.
- `migrations/00008_create_wanted_posts.sql` adalah contoh pola idempoten:
  `PRIMARY KEY` + `INSERT ... ON CONFLICT DO NOTHING`, dengan `ON DELETE
  CASCADE` dari tabel induk.
- Migrasi memakai goose; nomor berikutnya adalah `00011`.
- `api/openapi.yaml` dan `README.md` adalah kontrak API yang wajib ikut
  diperbarui.

## Sumber data katalog: repo, bukan database

Katalog tools tidak disimpan di Postgres. Alurnya:

```text
raw.githubusercontent.com/swegrowthid/tools-ai-swe-growth/main
  providers.md + codingagents.md + ade.md
    -> tools.Service.Sync (start + tiap 00:00 waktu server)
      -> parseTools
        -> tools.MemoryStore.ReplaceAll (snapshot in-memory)
          -> GET /tools, GET /tools/{id}, GET /tools/categories
```

Bukti di repo:

- `internal/tools/service.go` menetapkan `defaultRawBase` ke
  `raw.githubusercontent.com` dan `Sync` mengambil tiga file itu.
- `internal/app/app.go` selalu memakai `tools.NewMemoryStore()`; tidak ada
  cabang `pool != nil` yang membuat store Postgres untuk tools.
- `migrations/` tidak punya tabel `tools`; daftar tabel berhenti di `users`,
  `refresh_tokens`, `categories`, `posts`, `post_categories`, `post_items`,
  `news`, `wanted_posts`, dan `magic_links` (migration `00010` hanya menambah
  kolom kode di `magic_links`).
- Snapshot hilang saat proses restart lalu diisi ulang oleh sync. Kalau
  sumber gagal saat start, katalog kosong sampai sync berikutnya berhasil.

Konsekuensi untuk fitur sosial:

- `tool_id` adalah natural key dari markdown (`P-001`, `CA-018`, `ADE-012`).
  Tidak ada foreign key ke tabel katalog karena tabelnya tidak ada.
- Validasi keberadaan tool dilakukan di service terhadap snapshot, bukan di
  database.
- Vote/komentar untuk tool yang sementara hilang dari sumber tetap disimpan,
  tetapi tidak ditampilkan sampai tool itu muncul lagi.
- Kalau ID sumber berubah atau dipakai ulang untuk tool lain, data sosial lama
  ikut menempel ke ID itu. Kontrak ID di repo sumber harus stabil.

Alternatif "mirror katalog ke tabel `tools`" sengaja tidak dipilih: tabel itu
menduplikasi sumber kebenaran, butuh upsert/delete tiap sync, dan membuat vote
atau komentar bisa ikut terhapus saat sync. Untuk 259 baris katalog, snapshot
in-memory sudah cukup.

## Tujuan

1. Menyimpan vote secara durable di Postgres.
2. Menjamin satu user hanya punya satu vote per tool di level database.
3. Menampilkan jumlah vote dan status vote user pada list/detail tool.
4. Membuat tambah/hapus vote idempoten dan aman terhadap request bersamaan.
5. Menjaga katalog tetap read-only dari markdown; vote tidak mengubah katalog.
6. Menambah test, dokumentasi, dan kontrak OpenAPI.

## Di luar scope

- Downvote, skor +1/-1, atau rating 1-5.
- Vote anonim atau vote berbasis cookie/IP.
- Riwayat audit lengkap setiap perubahan vote.
- Notifikasi, leaderboard terpisah, atau analitik produk.
- Moderasi vote, shadow-ban, atau deteksi bot lanjutan.
- Denormalized counter (`vote_count`) di tabel katalog.
- Perubahan pada sync markdown, parser, atau scheduler tools.
- Implementasi komentar di tahap vote ini; hanya desainnya yang disiapkan.

## Keputusan desain

| Topik | Keputusan | Alasan |
| --- | --- | --- |
| Semantik vote | Usage vote: "saya memakai tool ini" | Sesuai permintaan; bukan rating. |
| Identitas | Wajib login; `user_id` dari JWT, bukan body | Mencegah double vote dan vote atas nama orang lain. |
| Kardinalitas | Satu baris per `(tool_id, user_id)` | Satu user satu vote per tool. |
| Tabel | `tool_votes` | Terpisah dari katalog; sumber kebenaran vote. |
| Tambah vote | `POST /tools/{id}/votes`, idempoten | Request ulang tidak menambah baris. |
| Cabut vote | `DELETE /tools/{id}/votes`, idempoten | User boleh berubah pikiran. |
| Hapus vote | Hard delete | Riwayat tidak dibutuhkan di v1. |
| Counter | Hitung `COUNT(*)` saat baca | Skala komunitas kecil; hindari counter drift. |
| Katalog | Tetap markdown/in-memory; tidak ada FK ke tools | Menghindari duplikasi sumber dan sync tabel katalog. |
| Validasi tool | Cek ke snapshot `tools.Store.Get` | Mencegah vote untuk ID yang tidak ada. |
| Tool hilang dari sumber | Vote disimpan, tetapi tidak dihitung/ditampilkan | Tidak ada kehilangan data jika tool muncul lagi. |
| Auth baca | `GET` publik; Bearer opsional hanya untuk mengisi `voted` | Hasil vote tampil tanpa login; `votes` selalu ada, `voted=false` untuk anonim atau token invalid. |
| Sort | `sort=votes`, default `desc` | Menampilkan tool paling banyak dipakai lebih dulu. |
| Paket | Perluas `internal/tools`, bukan paket baru | Vote adalah fitur katalog; menghindari siklus dependency. |

## Skema database

Riset lengkap dan alternatif yang ditolak ada di "Lampiran: riset dan
alternatif" di akhir dokumen.

File baru: `migrations/00011_create_tool_votes.sql`

```sql
-- +goose Up
-- Satu baris = satu user menyatakan memakai satu tool.
-- Katalog tools tidak ada di SQL; tool_id divalidasi service terhadap
-- snapshot markdown. Karena itu tidak ada foreign key ke tabel tools.
CREATE TABLE tool_votes (
  tool_id    TEXT NOT NULL,
  user_id    UUID NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),

  CONSTRAINT tool_votes_pkey PRIMARY KEY (tool_id, user_id),
  CONSTRAINT tool_votes_user_fk
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
  CONSTRAINT tool_votes_tool_id_check CHECK (
    char_length(tool_id) BETWEEN 3 AND 32
    AND tool_id ~ '^[A-Z]+-[0-9]+$'
  )
);

-- Query per user: "tool apa yang saya vote" dan cascade delete user.
CREATE INDEX idx_tool_votes_user_id ON tool_votes (user_id);

-- +goose Down
DROP TABLE IF EXISTS tool_votes;
```

Catatan skema:

- Tidak ada `updated_at`: vote tidak punya state yang di-update. Unvote lalu
  vote lagi menghasilkan `created_at` baru karena baris dihapus lalu dibuat.
- Tidak ada trigger `set_updated_at`.
- `tool_id` disimpan uppercase, sudah dinormalisasi service. `CHECK` harus
  diselaraskan dengan `toolIDPattern` di `internal/tools/parser.go`.
- `user_id` wajib UUID dan mereferensi `users(id)`.
- Down migration cukup drop table; index ikut terhapus.
- Tidak ada backfill; tabel baru mulai kosong.

## Pola query

### Tambah vote

```sql
INSERT INTO tool_votes (tool_id, user_id)
VALUES ($1, $2)
ON CONFLICT (tool_id, user_id) DO NOTHING;
```

- `RowsAffected() == 1` berarti vote baru.
- `RowsAffected() == 0` berarti user sudah vote; bukan error.
- Foreign key violation SQLSTATE `23503` berarti user tidak ada; map ke
  `ErrUnknownUser`. Handler membalas `401`, karena identitas dari token sudah
  tidak valid.

### Cabut vote

```sql
DELETE FROM tool_votes
WHERE tool_id = $1 AND user_id = $2;
```

- `RowsAffected() == 0` bukan error; response tetap sukses dan idempoten.

### Jumlah vote + status user untuk sekumpulan tool

```sql
SELECT
  tool_id,
  count(*)::int AS votes,
  coalesce(bool_or(user_id = NULLIF($2::text, '')::uuid), false) AS voted
FROM tool_votes
WHERE tool_id = ANY($1::text[])
GROUP BY tool_id;
```

- `$1` adalah `[]string` ID tool pada halaman/filter.
- `$2` adalah user ID viewer; string kosong untuk anonim.
- `NULLIF($2, '')::uuid` membuat perbandingan menjadi NULL untuk anonim,
  sehingga `bool_or` menghasilkan NULL lalu `coalesce` menjadi `false`.
- Tool tanpa vote tidak muncul di hasil; service mengisi default `votes=0`,
  `voted=false`.
- Jika `toolIDs` kosong, service tidak menjalankan query.

### Vote milik satu user (belum dipakai)

```sql
SELECT tool_id, created_at
FROM tool_votes
WHERE user_id = $1
ORDER BY created_at DESC, tool_id;
```

Belum dipakai endpoint v1, tetapi index `user_id` sudah mendukungnya.

### Catatan implementasi sort

`sort=votes` tidak boleh diteruskan ke `Store.List`: store snapshot hanya
mengerti `name` dan `updated`, dan vote baru diisi setelah `Store.List`
kembali. Alurnya:

1. `normalizeListOptions` menerima `SortVotes`, default `OrderDesc`.
2. Khusus `sort=votes`, `ListFor` memanggil `Store.List` dengan `Sort` dan
   `Order` dikosongkan supaya baris tetap urut katalog. Sort `name`/`updated`
   tetap diteruskan ke store.
3. Service mengisi `votes`/`voted` untuk semua baris hasil filter.
4. Service mengurutkan sendiri: vote desc/asc, tie-break `id` ascending.
5. Setelah itu `pageRows` memotong halaman dan menghitung `meta.total`.

Dengan urutan ini, `meta.total` tetap jumlah baris yang cocok, bukan jumlah
vote, dan page boundary tidak mengulang atau menghilangkan baris.

## Aturan bisnis

1. `tool_id` di path dinormalisasi uppercase dan di-trim.
2. Tool harus ada di snapshot saat vote; jika tidak, `404 tool not found`.
3. `POST` pertama menghasilkan `201`; `POST` duplikat menghasilkan `200`.
4. `DELETE` selalu `200` dengan ringkasan terbaru, termasuk jika vote tidak ada.
5. User ID selalu dari `auth.Principal`, tidak pernah dari body.
6. `GET /tools` dan `GET /tools/{id}` bisa diakses tanpa token. Jika token
   valid, `voted` diisi; jika tidak ada token atau token invalid, `voted=false`.
7. `votes` selalu jumlah seluruh user yang vote tool tersebut, bukan hanya
   user yang sedang login.
8. Tool yang hilang dari snapshot tidak ditampilkan, tetapi baris vote-nya
   tetap disimpan. Jika tool muncul lagi, vote lama kembali terhitung.
9. Tidak ada endpoint untuk melihat daftar user yang vote (privasi).
10. Tidak ada rate limit tambahan di v1; constraint unik dan auth sudah
    mencegah double vote. Jika ada abuse akun ganda, tangani di tahap lanjut.

## Kontrak API

### Ringkasan endpoint

| Method | Path | Auth | Response |
| --- | --- | --- | --- |
| `GET` | `/tools` | opsional | `ToolList`; tiap `Tool` punya `votes` dan `voted` |
| `GET` | `/tools/{id}` | opsional | `Tool` dengan `votes` dan `voted` |
| `POST` | `/tools/{id}/votes` | wajib | `201` vote baru / `200` duplikat, body `VoteResult` |
| `DELETE` | `/tools/{id}/votes` | wajib | `200`, body `VoteResult` |

`GET /tools` dan `GET /tools/{id}` tidak pernah membalas `401` karena token:
tanpa token hasilnya tetap sama, hanya `voted` yang bernilai `false`. Endpoint
`POST`/`DELETE` memakai `RequireAuth`, jadi token absen atau invalid di sana
tetap `401`.

### `VoteResult`

```json
{
  "tool_id": "P-001",
  "votes": 12,
  "voted": true
}
```

- `tool_id`: ID uppercase dari sumber.
- `votes`: jumlah vote terbaru untuk tool.
- `voted`: status user yang meminta setelah operasi.

### `GET /tools` dan `GET /tools/{id}`

Tambahan field pada `Tool`:

```json
{
  "id": "P-001",
  "category": "providers",
  "name": "OpenAI",
  "votes": 12,
  "voted": true
}
```

- `votes` selalu ada, minimal `0`.
- `voted` selalu ada; `false` untuk anonim.
- `sort=votes` menerima `order=asc|desc`; default `desc`.
- Tie-break `sort=votes` memakai `id` ascending, konsisten dengan sort lain.
- `meta.total` tetap menghitung baris katalog yang cocok, bukan jumlah vote.

### Error

| Status | Kondisi | Body |
| --- | --- | --- |
| `400` | Parameter list invalid | `invalid input` |
| `401` | POST/DELETE tanpa token atau token invalid | pesan auth middleware |
| `401` | `user_id` dari token tidak ada di database | `invalid or expired token` |
| `404` | Tool ID tidak ada di snapshot | `tool not found` |
| `500` | Database atau error internal | `internal error` |

Catatan: `POST` duplikat bukan `409`; sengaja idempoten seperti pola
`wanted_posts`. `DELETE` mengembalikan `200` + `VoteResult` (bukan `204`)
supaya UI langsung menerima count terbaru setelah unvote.

## Arsitektur dan call graph

Vote tetap di paket `internal/tools` karena masih satu domain katalog. Tidak
perlu paket baru yang harus mengimpor `tools` dan membuat wiring melingkar.

```text
POST /tools/{id}/votes
  -> auth.Middleware.RequireAuth
    -> tools.Handler.vote
      -> tools.Service.Vote
        -> tools.Service.lookup
          -> tools.Store.Get (validasi ID terhadap snapshot)
        -> tools.VoteStore.Add
          -> PostgresVoteStore.Add
            -> INSERT INTO tool_votes ... ON CONFLICT DO NOTHING
        -> tools.VoteStore.Summaries
          -> SELECT count(*) ... GROUP BY tool_id

DELETE /tools/{id}/votes
  -> auth.Middleware.RequireAuth
    -> tools.Handler.unvote
      -> tools.Service.Unvote
        -> lookup + VoteStore.Remove + VoteStore.Summaries

GET /tools
  -> auth.Middleware.OptionalAuth
    -> tools.Handler.list
      -> tools.Service.ListFor
        -> tools.Store.List (snapshot; sort dikosongkan hanya untuk sort=votes)
        -> tools.VoteStore.Summaries (semua ID hasil filter)
        -> sort by votes + page

GET /tools/{id}
  -> auth.Middleware.OptionalAuth
    -> tools.Handler.get
      -> tools.Service.GetFor
        -> tools.Store.Get (snapshot)
        -> tools.VoteStore.Summaries (satu ID)
```

### Wiring app

- `pool == nil` (test, `cmd/memserver`): `tools.NewMemoryVoteStore()`.
  Vote tidak durable di mode ini; data hilang saat proses mati.
- `pool != nil` (produksi): `tools.NewPostgresVoteStore(pool.DB)`.
- Snapshot katalog tetap `tools.NewMemoryStore()` di kedua mode.
- `tools.NewServiceWithVotes(snapshotStore, voteStore)`.
- `tools.Handler.RegisterRoutesWithAuth(mux, requireAuth, optionalAuth)`.
- `auth.Middleware.OptionalAuth` baru; `RequireAuth` tetap dipakai untuk
  endpoint tulis.
- CORS yang ada sudah mengizinkan header `Authorization` serta method `GET`,
  `POST`, dan `DELETE`; fitur vote tidak butuh perubahan CORS.

### Interface yang diusulkan

```go
// VoteStore adalah port persistence vote. Service tidak tahu SQL.
type VoteStore interface {
    Add(ctx context.Context, userID, toolID string) (created bool, err error)
    Remove(ctx context.Context, userID, toolID string) (removed bool, err error)
    Summaries(ctx context.Context, viewerID string, toolIDs []string) (map[string]VoteSummary, error)
}

type VoteSummary struct {
    Votes int
    Voted bool
}
```

`Tool` mendapat field view-only:

```go
Votes int  `json:"votes"`
Voted bool `json:"voted"`
```

Field ini diisi service saat response; parser/sync selalu meninggalkannya
`0`/`false` sehingga snapshot tidak menyimpan state vote.

## Perubahan per file

| File | Perubahan |
| --- | --- |
| `migrations/00011_create_tool_votes.sql` | Baru. Tabel `tool_votes`, PK, FK, CHECK, index `user_id`. |
| `internal/tools/tools.go` | Tambah `Votes`/`Voted` di `Tool`, `SortVotes`, `VoteSummary`, `VoteResult`, `VoteStore`, `ErrUnknownUser`; update komentar paket. |
| `internal/tools/service.go` | Tambah field `votes`, `NewServiceWithVotes`, `ListFor`, `GetFor`, `Vote`, `Unvote`, enrichment, sort vote. `List`/`Get` lama menjadi wrapper anonim. |
| `internal/tools/vote_store_memory.go` | Baru. Memory store dengan `RWMutex`, index per tool dan per user, idempoten. |
| `internal/tools/vote_store_postgres.go` | Baru. `database/sql` + `pgx`, `ON CONFLICT`, `ANY($1::text[])`, mapping FK violation. |
| `internal/tools/http.go` | Tambah `RegisterRoutesWithAuth`, route vote, optional auth, `voted` viewer, error mapping. `RegisterRoutes` lama tetap untuk test. |
| `internal/auth/middleware.go` | Tambah `OptionalAuth`: token valid mengisi principal; token absen/invalid lanjut anonim. |
| `internal/app/app.go` | Pilih memory/Postgres vote store; wiring `NewServiceWithVotes` dan `RegisterRoutesWithAuth`. Update komentar katalog. |
| `internal/tools/*_test.go` | Test memory store, service, handler, sort, idempotensi, race, auth. |
| `internal/auth/*_test.go` | Test `OptionalAuth`. |
| `internal/app/app_test.go` | Test route vote lewat graph produksi (auth + 404 snapshot kosong). |
| `api/openapi.yaml` | Field `votes`/`voted`, `sort=votes`, path vote, schema `VoteResult`, security opsional. |
| `README.md` | Endpoint, aturan vote, arsitektur, tabel migrasi `00011`. |
| `go.mod`/`go.sum` | Tidak berubah; tidak ada dependency baru. |

Tidak ada file komentar di daftar ini. Desain komentar hanya ada di bagian
"Persiapan komentar tools" dan belum masuk scope implementasi vote.

## Langkah implementasi

Status: belum ada langkah yang dikerjakan. Urutan di bawah dipakai saat
implementasi disetujui.

- [ ] Tulis `migrations/00011_create_tool_votes.sql`; jalankan `make migrate-up`
      dan cek `\d+ tool_votes`.
- [ ] Tambah tipe dan interface vote di `internal/tools/tools.go`.
- [ ] Implementasikan `MemoryVoteStore` beserta test unit.
- [ ] Implementasikan `PostgresVoteStore` beserta test integrasi/manual.
- [ ] Ubah `Service`: `NewServiceWithVotes`, `ListFor`, `GetFor`, `Vote`,
      `Unvote`, enrichment, dan `sort=votes`.
- [ ] Tambah `OptionalAuth` di `internal/auth/middleware.go` dan test.
- [ ] Ubah handler: route vote, principal viewer, status `201/200`, error map.
- [ ] Wiring `internal/app/app.go` untuk memory/Postgres vote store.
- [ ] Perbarui `api/openapi.yaml`: schema, parameter, path, security.
- [ ] Perbarui `README.md`: endpoint, aturan, migrasi, arsitektur.
- [ ] Jalankan seluruh test dan manual E2E dengan Postgres.
- [ ] Review EXPLAIN query agregasi dan pastikan index terpakai.
- [ ] Tidak menambah migration, route, atau tipe komentar di PR vote ini;
      desain komentar tetap di dokumen plan.

## Testing

### Unit dan handler

- Memory store:
  - vote pertama `created=true`; vote kedua `created=false`.
  - dua user berbeda menghasilkan count 2.
  - remove vote mengurangi count; remove kedua tetap sukses.
  - `Summaries` mengembalikan count dan `voted` yang benar untuk viewer.
- Service:
  - vote tool tidak dikenal -> `ErrNotFound`.
  - vote tanpa login (userID kosong) -> `ErrInvalidInput`.
  - `ListFor` mengisi `votes`/`voted` untuk semua baris.
  - `ListFor` anonim: count terisi, `voted=false`.
  - `sort=votes` default desc; tie-break ID ascending.
  - `sort=votes&order=asc` berfungsi.
  - page/meta tidak berubah karena vote.
- Handler:
  - `POST` tanpa token -> 401.
  - `POST` dengan principal -> 201, lalu 200 untuk duplikat.
  - `DELETE` -> 200 dan `voted=false`; delete lagi tetap 200.
  - tool tidak dikenal -> 404.
  - `GET /tools` dengan token -> `voted=true` pada tool yang sudah divote.
  - `GET /tools` tanpa token -> `voted=false`, count tetap benar.
- Auth:
  - `OptionalAuth` dengan token valid mengisi principal.
  - token invalid/absen tidak mengisi principal dan tidak menolak request.
  - `RequireAuth` tetap 401 untuk token invalid.
- App:
  - route vote terdaftar; tanpa token 401; dengan token dan snapshot kosong 404.
- Race:
  - dua `POST` bersamaan untuk pasangan yang sama hanya menghasilkan satu
    baris; jalankan `go test -race ./internal/tools`.

### Integrasi Postgres / manual

1. `make migrate-up`.
2. `psql "$DB_URL" -c '\d+ tool_votes'`; pastikan:
   - PK `(tool_id, user_id)`,
   - FK `user_id -> users(id) ON DELETE CASCADE`,
   - CHECK format tool ID,
   - index `idx_tool_votes_user_id`.
3. Register dua user, login, vote tool yang sama:
   - user A `POST` -> 201, `votes=1`.
   - user A `POST` lagi -> 200, `votes=1`.
   - user B `POST` -> 201, `votes=2`.
4. `DELETE` user A -> 200, `votes=1`; `DELETE` lagi -> 200, `votes=1`.
5. `GET /tools?sort=votes&order=desc`; pastikan urutan dan `voted` benar.
6. `GET /tools` tanpa token; `votes` tetap ada, `voted=false`.
7. Hapus user lewat SQL (karena belum ada endpoint delete user), pastikan
   vote-nya ikut terhapus karena cascade. Access token lama masih valid secara
   tanda tangan; percobaan vote dengannya harus membalas `401` lewat
   `ErrUnknownUser`.
8. `make migrate-down`, lalu `make migrate-up`; pastikan bersih.

### Perintah verifikasi

```sh
gofmt -l ./cmd ./internal
go vet ./...
go test ./...
go test -race ./internal/tools ./internal/app
make migrate-up
make migrate-status
```

### Cek performa

Jalankan `EXPLAIN (ANALYZE, BUFFERS)` untuk query `Summaries` setelah mengisi
data contoh. Target:

- index `tool_votes_pkey` dipakai untuk filter `tool_id = ANY(...)`;
- tidak ada sequential scan pada tabel besar;
- query tetap di bawah ~10 ms untuk skala komunitas.

Jika suatu saat jumlah vote membesar dan query menjadi lambat, opsi lanjutan:

1. Cache count di memori dengan invalidasi saat vote/unvote.
2. Tabel counter `tool_vote_counts` + trigger/transaksi.
3. Materialized view yang di-refresh periodik.

Jangan menambah counter sebelum ada bukti beban; counter menambah kompleksitas
konsistensi.

## Rollout dan rollback

### Rollout

1. Deploy migrasi lebih dulu: `make migrate-up` (atau unit migrate di server).
2. Deploy API baru.
3. Verifikasi `/readyz`, lalu smoke test:
   - `GET /tools` masih 200 dan tanpa token tetap membawa `votes`;
   - vote dengan token 201;
   - vote duplikat 200;
   - delete 200.
4. Pantau error 500 pada log dan latency `GET /tools`.

Migrasi bersifat additive: API lama tidak terganggu dan tidak membaca tabel
baru. Tidak ada backfill.

### Rollback

1. Rollback API ke versi sebelumnya (versi lama mengabaikan `tool_votes`).
2. Jika ingin menghapus data vote juga, jalankan `make migrate-down`.
3. Jangan drop tabel saat API baru masih melayani request vote.

Kehilangan data: `migrate-down` menghapus seluruh vote. Jika rollback hanya
sementara, biarkan tabel tetap ada.

## Risiko dan mitigasi

| Risiko | Dampak | Mitigasi |
| --- | --- | --- |
| Vote ganda karena race | Count inflated | Composite PK + `ON CONFLICT DO NOTHING`; test race. |
| Vote untuk tool yang sudah dihapus | Data yatim tersembunyi; user tidak bisa unvote lewat API karena tool 404 | Vote tetap disimpan; jika perlu self-service cleanup, izinkan DELETE tanpa validasi snapshot atau tambahkan job cleanup. |
| ID tool dipakai ulang untuk tool berbeda | Vote lama ikut pindah | Kontrak ID sumber harus stabil; jika perlu, tambah snapshot `tool_name`/audit. |
| Akun ganda untuk inflate vote | Hasil tidak representatif | Auth + rate limit/verifikasi email di tahap lanjut; monitor anomali. |
| `GET /tools` bergantung pada DB | Katalog bisa 500 saat DB down | Terima untuk v1; opsi cache count / degrade di tahap lanjut. |
| Count exact melambat | List lambat | Index yang benar; ukur; baru tambah cache/counter. |
| Token invalid pada GET | `voted` hilang | Perilaku opsional auth; kirim token valid dari klien. |
| Migrasi belum jalan saat API baru start | Vote 500 | Urutan deploy migrasi dulu; readiness check. |

## Persiapan komentar tools (belum diimplementasikan)

Komentar **bisa** ditambahkan tanpa mengubah arsitektur katalog, dengan pola
yang sama seperti vote: katalog tetap dari markdown, komentar disimpan di SQL
dan memakai `tool_id` sebagai natural key. Tidak ada migration komentar di
tahap ini.

### Bentuk data yang disiapkan

Rencana tabel saat fitur disetujui (bukan migration sekarang):

```sql
tool_comments (
  id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tool_id    TEXT NOT NULL CHECK (
               char_length(tool_id) BETWEEN 3 AND 32
               AND tool_id ~ '^[A-Z]+-[0-9]+$'
             ),
  user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  body       TEXT NOT NULL CHECK (char_length(body) BETWEEN 1 AND 2000),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_tool_comments_tool_created
  ON tool_comments (tool_id, created_at DESC, id);
CREATE INDEX idx_tool_comments_user_id ON tool_comments (user_id);
```

- Flat comment dulu; balasan bisa ditambah belakangan lewat `parent_id`
  sebagai migration additive.
- `updated_at` memakai trigger `set_updated_at` yang sudah ada, sama seperti
  `users` dan `posts`.
- `ON DELETE CASCADE` dari `users` konsisten dengan vote. Kalau produk ingin
  komentar tetap tampil sebagai "[user dihapus]", ganti ke `SET NULL` dan
  buat `user_id` nullable.
- Body disimpan sebagai teks apa adanya. Kalau frontend merender markdown,
  sanitasi dilakukan di frontend; backend tidak merender HTML.
- Service memangkas spasi di ujung body, menolak body kosong dan byte NUL,
  lalu menegakkan batas 2000 karakter. Batas yang sama dijaga `CHECK` di
  database.
- Response komentar membawa `user.username`, bukan `email` atau
  `password_hash`. Kalau user dihapus dan komentar dipertahankan, tampilkan
  label "[user dihapus]" tanpa data pribadi.

### Kontrak API yang disiapkan

| Method | Path | Auth | Arti |
| --- | --- | --- | --- |
| `GET` | `/tools/{id}/comments` | tidak | Daftar komentar, terbaru dulu, dengan paging. |
| `POST` | `/tools/{id}/comments` | ya | Tulis komentar. `user_id` dari JWT. |
| `PUT` | `/tools/{id}/comments/{comment_id}` | ya | Edit komentar milik sendiri. |
| `DELETE` | `/tools/{id}/comments/{comment_id}` | ya | Hapus komentar milik sendiri. |

- Edit memakai `PUT`, bukan `PATCH`, karena CORS server saat ini hanya
  mengizinkan `GET`, `POST`, `PUT`, `DELETE`, dan `OPTIONS`.
- `GET /tools` bisa menambah `comment_count` nanti; field baru bersifat
  additive dan tidak merusak klien lama.
- Kalau nanti list ingin menampilkan cuplikan komentar terbaru, itu butuh
  query `LATERAL` per tool. Untuk v1 komentar, tampilkan count saja dan
  arahkan ke detail; cuplikan bisa ditambahkan tanpa mengubah skema.
- Validasi tool saat menulis komentar memakai snapshot yang sama dengan vote:
  `404 tool not found` kalau ID tidak ada.
- Komentar untuk tool yang hilang dari sumber disimpan tetapi tidak
  ditampilkan, sama seperti vote.
- Rate limit per user dan moderasi belum masuk desain v1; keduanya perlu
  diputuskan sebelum komentar dirilis.

### Integrasi dengan implementasi vote

- `CommentStore` adalah port terpisah dari `VoteStore`; keduanya bisa dipakai
  oleh service yang sama tanpa saling mengimpor.
- `Tool` bisa mendapat `comment_count` secara additive; klien lama tidak
  terpengaruh.
- Route komentar memakai pola auth yang sama: `GET` publik, `POST`/`PUT`/
  `DELETE` wajib login dan hanya pemilik yang boleh mengubah.
- `GET /tools` nanti menjalankan agregasi vote dan komentar. Untuk 259 baris
  katalog, dua query masih murah; gabungkan dengan CTE hanya jika profiling
  menunjukkan perlu.
- Jangan menggabungkan komentar ke `tool_votes`; lifecycle, bentuk payload,
  dan kebutuhan moderasinya berbeda.

### Yang tidak perlu disiapkan sekarang

- Tidak ada tabel, migration, route, atau tipe Go komentar di tahap vote.
- Tidak ada `comment_count` di response `Tool` sebelum fitur komentar ada.
- Tidak ada abstraksi `CommentStore` kosong; interface dibuat saat fitur
  dikerjakan agar tidak ada kode mati.

## Pertanyaan yang belum diputuskan

Masih perlu keputusan produk sebelum implementasi:

1. **Semantik vote**: apakah benar "saya memakai tool ini" (usage vote), bukan
   rating 1-5 atau upvote/downvote? Jika rating, skema butuh kolom `value`.
2. **Boleh unvote?** Plan ini memilih boleh. Jika vote permanen, hapus
   endpoint DELETE dan baris tidak pernah dihapus.
3. **Retensi saat tool hilang**: plan ini menyimpan vote dan menyembunyikannya.
   Jika ingin dibersihkan otomatis, tambahkan job cleanup setelah sync sukses.
4. **Sort default**: plan ini memakai `desc` untuk `sort=votes`. Jika UI ingin
   urutan katalog tetap default, tidak ada perubahan pada default lama.
5. **Apakah vote perlu ditampilkan di kartu kategori?** Plan ini tidak
   menambahkan agregasi vote per kategori.
6. **Komentar**: flat dulu atau perlu balasan (`parent_id`) sejak v1? Apakah
   komentar user yang dihapus ikut hilang atau tetap tampil? Ini diputuskan
   saat fitur komentar dikerjakan, bukan di tahap vote.

## Definition of Done

- [ ] Migrasi `00011` jalan dan rollback bersih.
- [ ] `POST`/`DELETE` vote berfungsi, idempoten, dan butuh auth.
- [ ] `GET /tools` dan detail membawa `votes`/`voted`.
- [ ] `sort=votes` berfungsi dengan paging dan tie-break stabil.
- [ ] Tidak ada double vote pada request bersamaan.
- [ ] OpenAPI dan README selaras.
- [ ] `gofmt`, `go vet`, `go test`, `go test -race` hijau.
- [ ] Manual E2E dengan Postgres lulus.
- [ ] Tidak ada dependency baru.
- [ ] Tidak ada migration/tabel/route komentar di PR ini.

## Estimasi

- Skema + store memory/Postgres: ~0.5 hari.
- Service + handler + optional auth: ~1 hari.
- Test, OpenAPI, README, manual E2E: ~1 hari.
- Total vote: sekitar 2-3 hari kerja.
- Komentar belum diestimasi; dikerjakan setelah desainnya disetujui.

## Lampiran: riset dan alternatif

### Temuan riset skema

1. **Satu vote per user per item harus dijaga constraint database.**
   Pengecekan "sudah vote?" di application layer saja tidak cukup: dua request
   bersamaan bisa sama-sama lolos cek lalu menulis dua baris. PostgreSQL
   menyediakan `UNIQUE`/`PRIMARY KEY` yang otomatis membuat index B-tree unik.
   Pola umum vote table memakai `UNIQUE (user_id, item_id)` atau composite
   primary key pada pasangan tersebut.

2. **Junction table adalah model yang tepat.**
   Vote adalah relasi many-to-many antara `users` dan tool. Tabel pasangan
   dengan composite primary key adalah pola standar untuk junction table.
   Tidak perlu surrogate `id` karena tidak ada tabel lain yang akan mereferensi
   satu baris vote.

3. **Urutan kolom composite key sebaiknya mengikuti query dominan.**
   Query terpanas adalah agregasi per tool untuk `GET /tools`: filter
   `tool_id = ANY(...)`, lalu `GROUP BY tool_id`. Karena itu primary key
   `(tool_id, user_id)` lebih menguntungkan. Index tambahan pada `user_id`
   dibutuhkan untuk lookup per user dan untuk `ON DELETE CASCADE` saat user
   dihapus. PostgreSQL tidak otomatis membuat index pada kolom foreign key.

4. **Idempotensi tulis paling aman lewat `ON CONFLICT DO NOTHING`.**
   PostgreSQL menjamin `INSERT ... ON CONFLICT` menangani konflik unique tanpa
   error. `RowsAffected` membedakan vote baru (1) dari vote duplikat (0).
   `DELETE` yang tidak menemukan baris juga idempoten secara alami.

5. **`COUNT(*)` exact bisa memindai banyak baris.**
   PostgreSQL perlu memeriksa visibilitas baris, jadi count exact tidak bisa
   hanya membaca metadata. Untuk jumlah tool ~259 dan vote komunitas kecil,
   `GROUP BY tool_id` dengan index masih murah. Denormalized counter bisa
   memberi O(1), tetapi menambah risiko drift dan reconciliation. Tunda sampai
   ada bukti beban.

6. **`CHECK` tidak bisa memvalidasi keberadaan tool di tabel lain.**
   Karena katalog tidak punya tabel SQL, `tool_id` tidak bisa memakai foreign
   key. Validasi keberadaan harus di service terhadap snapshot. `CHECK` tetap
   berguna untuk memastikan format ID sesuai parser (`P-001`, `CA-018`,
   `ADE-012`).

7. **`ON DELETE CASCADE` cocok untuk vote milik user.**
   Vote tidak punya makna tanpa user. Jika user dihapus, baris vote ikut
   dihapus. Ini juga mencegah vote yatim saat akun dibersihkan.

### Sumber

- PostgreSQL 18, Constraints (unique, primary key, foreign key, cascade):
  https://www.postgresql.org/docs/current/ddl-constraints.html
- PostgreSQL 18, INSERT / `ON CONFLICT`:
  https://www.postgresql.org/docs/current/sql-insert.html
- PostgreSQL 18, Multicolumn Indexes:
  https://www.postgresql.org/docs/current/indexes-multicolumn.html
- PostgreSQL Wiki, Slow Counting:
  https://wiki.postgresql.org/wiki/Slow_Counting
- Haki Benita, "How to Get or Create in PostgreSQL" (ON CONFLICT dan race):
  https://hakibenita.com/postgresql-get-or-create
- Pola vote table `UNIQUE (user_id, item_id)` (contoh MySQL/PHP):
  https://hideyukimori.github.io/NENE2/howto/voting-system
- Pola junction table + composite key + catatan denormalized counter:
  https://oneuptime.com/blog/post/2026-03-31-mysql-likes-votes-system/view
- Pembahasan desain upvote/downvote dan composite key:
  https://stackoverflow.com/questions/44987661/database-design-for-upvote-downvote-in-discussion-forum

### Implikasi untuk repo ini

- Tabel `tool_votes` menjadi satu-satunya tabel SQL untuk fitur tools pada
  tahap vote. Komentar nanti memakai tabel terpisah.
- Katalog tetap snapshot in-memory; `tool_id` adalah natural key dari sumber.
- Primary key `(tool_id, user_id)` + index `user_id`.
- Tidak ada kolom `vote_count` di tabel katalog.
- Validasi tool dilakukan sebelum insert.
- `voted` dihitung dari keberadaan baris user tersebut, bukan dari counter.

### Alternatif yang ditolak

| Alternatif | Masalah |
| --- | --- |
| Array/JSONB vote di `users` | Tidak bisa enforce satu vote per tool dengan constraint; agregasi mahal. |
| Kolom `vote_count` di tabel tools | Tidak ada tabel tools; sumber katalog markdown. Counter mudah drift. |
| Tabel `tools` mirror katalog | Menduplikasi sumber kebenaran, butuh upsert/delete saat sync, dan vote bisa ikut terhapus. |
| Event log `tool_vote_events` | Cocok untuk audit/analitik, tetapi overkill untuk v1. |
| Vote anonim berbasis cookie/IP | Mudah dimanipulasi; tidak memenuhi "satu orang satu vote". |
| Surrogate `id` pada vote | Tidak ada consumer yang butuh ID baris; composite key lebih sederhana. |
