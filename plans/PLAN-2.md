# Plan 2: Migrasi Goose + Skema User + Makefile (sweg-ai)

## Context

- Folder `migrations/` masih kosong. Belum ada runner migrasi.
- Arsitektur tetap: stdlib Go, `database/sql` + `pgx/v5/stdlib`, `DB_URL` Supabase direct.
- `db.Open` fail-fast. `PostgresStore.List` masih `SELECT id, name`.
- Keputusan user sudah kunci:
  1. Alat: `pressly/goose v3`.
  2. ID: `UUID`.
  3. `username` dan `email` wajib unik.
  4. Simpan `password_hash` saja, bukan password plain.
  5. `updated_at` update otomatis via trigger.
  6. Jalankan via `make`. Setup juga `make run` untuk app.

## Approach

- Tambah `goose v3` sebagai satu-satunya dep baru.
- Satu migrasi awal: `migrations/00001_create_users.sql` (Up + Down).
- Skema `users` untuk komunitas kecil: id, username, email, password_hash, created_at, updated_at.
- Trigger `set_updated_at` isi `NEW.updated_at = now()` tiap `UPDATE`.
- Runner tipis `cmd/migrate/main.go`: load config, open pool, panggil goose via filesystem.
- `Makefile`: `run`, `migrate-up`, `migrate-down`, `migrate-status`, `vet`, `test`.
- Selaraskan kode baca: perluas `User`, perbaiki `List`. Tanpa auth dulu.

Call graph (migrasi):

```text
make migrate-up
  -> cmd/migrate
    -> config.Load
      -> db.Open
        -> goose.Up
          -> migrations/00001_create_users.sql
```

Call graph (app baca):

```text
HTTP handlers
  -> user.Handler
    -> user.Service
      -> user.PostgresStore
        -> db.Pool
```

## Files to modify

- `migrations/00001_create_users.sql` - buat. Isi Up + Down.
- `cmd/migrate/main.go` - buat. Runner tipis goose.
- `Makefile` - buat. Target run + migrate + vet + test.
- `go.mod` + `go.sum` - tambah `github.com/pressly/goose/v3`.
- `internal/user/user.go` - perluas struct `User`.
- `internal/user/store_postgres.go` - sesuaikan `SELECT` dan `Scan`.
- `api/openapi.yaml` - tambah skema `User` (tanpa hash).
- `.env-example` - tambah komentar contoh `make` (tanpa ubah `DB_URL=`).

## Reuse

- `internal/platform/db/db.go` - `db.Open` dan `Pool` sudah ada. Goose pakai `pool.DB`.
- `internal/platform/config/config.go` - `Load()` baca `DB_URL`. Dipakai `cmd/api` dan `cmd/migrate`.
- `internal/user/store_postgres.go` - pola `QueryContext` + `Scan` + `%w`.
- `cmd/api/main.go` - pola fail-fast `os.Exit(1)` saat DB gagal.
- `plans/PLAN-1.md` - aturan pool limit dan ping.

## Skema SQL (draf final)

```sql
-- +goose Up
CREATE EXTENSION IF NOT EXISTS "pgcrypto";

CREATE TABLE users (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  username TEXT NOT NULL UNIQUE CHECK (char_length(username) >= 3),
  email TEXT NOT NULL UNIQUE CHECK (email LIKE '%@%'),
  password_hash TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE OR REPLACE FUNCTION set_updated_at()
RETURNS TRIGGER AS $$
BEGIN
  NEW.updated_at = now();
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_users_updated_at
BEFORE UPDATE ON users
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- +goose Down
DROP TRIGGER IF EXISTS trg_users_updated_at ON users;
DROP FUNCTION IF EXISTS set_updated_at();
DROP TABLE IF EXISTS users;
```

## Bentuk User baru (draf)

```go
type User struct {
  ID           string    `json:"id"`
  Username     string    `json:"username"`
  Email        string    `json:"email"`
  PasswordHash string    `json:"-"`
  CreatedAt    time.Time `json:"created_at"`
  UpdatedAt    time.Time `json:"updated_at"`
}
```

- `List` hanya `SELECT id, username, email, created_at, updated_at`. Jangan expose hash.
- `PasswordHash` tetap ada di struct dengan `json:"-"` untuk create/login nanti.

## Makefile (draf)

```make
run:
	go run ./cmd/api

migrate-up:
	go run ./cmd/migrate up

migrate-down:
	go run ./cmd/migrate down

migrate-status:
	go run ./cmd/migrate status

vet:
	go vet ./...

test:
	go test ./...
```

## cmd/migrate (draf)

- `main`: `cfg := config.Load()`, `pool, err := db.Open(ctx, cfg.DBURL)`.
- `goose.SetDialect("postgres")`.
- Baca `os.Args[1]`: `up`, `down`, `status`. Default `up`.
- Panggil `goose.Up(pool.DB, "migrations")` atau `Down` / `Status`.
- Gagal = log + `os.Exit(1)`. Sukses = log `ok`.
- Tanpa embed dulu. Filesystem cukup. Embed opsional tahap lanjut.

## Steps

- [x] Tambah dep `github.com/pressly/goose/v3`. Jalankan `go mod tidy`.
- [x] Tulis `migrations/00001_create_users.sql` sesuai draf di atas.
- [x] Tulis `cmd/migrate/main.go` sesuai draf di atas.
- [x] Tulis `Makefile` sesuai draf di atas.
- [x] Perluas `internal/user/user.go` sesuai draf di atas.
- [x] Sesuaikan `internal/user/store_postgres.go`: query kolom baru, scan 5 kolom.
- [x] Tambah skema `User` di `api/openapi.yaml` tanpa `password_hash`.
- [x] Tambah komentar `make migrate-up` di `.env-example`.
- [x] Uji berurutan: `make migrate-status`, `make migrate-up`, `make migrate-status`.
- [x] Cek `psql \d users`: PK UUID, 2 unique, trigger ada.
- [x] Uji rollback: `make migrate-down`, lalu `make migrate-up` lagi.
- [x] Verifikasi akhir: `make vet`, `make test`, `make run` + `curl /users`.

## Verification

Hasil eksekusi 2026-09-20 (semua lulus):

- `make migrate-status` tampilkan versi goose benar.
- `make migrate-up` buat tabel `users` tanpa galat.
- Skema `public.users`: 6 kolom, PK UUID, unique username+email, trigger aktif.
- `UPDATE` geser `updated_at` otomatis (teruji: true).
- `make migrate-down` hapus tabel. `make migrate-up` buat ulang bersih.
- `make vet` bersih. `make test` lulus.
- `GET /users` balas 200. Tanpa hash. Insert budi balas bentuk baru.
- Tanpa `DB_URL`: migrate tolak start dengan pesan jelas.

Catatan: fungsi dan trigger goose perlu blok `StatementBegin/End`.
Tanpa itu goose pecah statement di `$$` dan migrasi gagal.
